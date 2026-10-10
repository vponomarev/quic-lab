package backup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrBusy = errors.New("backup busy or retention limit reached")
var ErrUnavailable = errors.New("backup server unavailable")
var ErrNotFound = errors.New("backup not available")
var jobIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type Job struct {
	ID            string    `json:"id"`
	State         string    `json:"state"`
	Kind          Kind      `json:"type"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	SizeBytes     int64     `json:"size_bytes"`
	SHA256        string    `json:"sha256"`
	ErrorCode     string    `json:"error_code,omitempty"`
	ServerVersion string    `json:"server_version"`
}
type jobState struct {
	Job
	readers int
	deleted bool
}
type Manager struct {
	mu           sync.Mutex
	root         string
	coordinator  *Coordinator
	quota        int64
	jobs         map[string]*jobState
	requests     map[string]string
	busy, closed bool
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	owner        *os.File
}

func NewManager(root string, c *Coordinator, quotaBytes int64) (*Manager, error) {
	if quotaBytes <= 0 {
		return nil, errors.New("backup quota must be positive")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("unsafe backup directory")
	}
	owner, err := AcquireProcessLock(root, ".manager.lock")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{root: root, coordinator: c, quota: quotaBytes, jobs: map[string]*jobState{}, requests: map[string]string{}, ctx: ctx, cancel: cancel, owner: owner}
	success := false
	defer func() {
		if !success {
			cancel()
			owner.Close()
		}
	}()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		n := entry.Name()
		if strings.HasPrefix(n, ".partial-") && !entry.IsDir() {
			if err = os.Remove(filepath.Join(root, n)); err != nil {
				return nil, err
			}
			continue
		}
		if !strings.HasSuffix(n, ".json") {
			continue
		}
		id := strings.TrimSuffix(n, ".json")
		if !jobIDPattern.MatchString(id) {
			continue
		}
		info, e := entry.Info()
		if e != nil || !info.Mode().IsRegular() || info.Size() > 16384 {
			return nil, errors.New("invalid backup job metadata")
		}
		raw, e := os.ReadFile(filepath.Join(root, n))
		if e != nil {
			return nil, e
		}
		var j Job
		if json.Unmarshal(raw, &j) != nil || j.ID != id {
			return nil, errors.New("invalid backup job metadata")
		}
		if j.State != "ready" {
			j.State = "error"
			j.ErrorCode = "interrupted"
			os.Remove(filepath.Join(root, id+".age"))
		}
		m.jobs[id] = &jobState{Job: j}
	}
	snapshots, e := os.ReadDir(c.root)
	if e != nil {
		return nil, e
	}
	for _, entry := range snapshots {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".backup-snapshot-") {
			if e = os.RemoveAll(filepath.Join(c.root, entry.Name())); e != nil {
				return nil, e
			}
		}
	}
	m.cleanupLocked(time.Now())
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		timer := time.NewTicker(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-m.ctx.Done():
				return
			case now := <-timer.C:
				m.mu.Lock()
				m.cleanupLocked(now)
				m.mu.Unlock()
			}
		}
	}()
	success = true
	return m, nil
}
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	m.owner.Close()
}
func (m *Manager) saveLocked(j Job) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.root, ".partial-metadata-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(m.root, j.ID+".json"))
	}
	if err == nil {
		err = syncDirectory(m.root)
	}
	return err
}
func (m *Manager) cleanupLocked(now time.Time) {
	for id, s := range m.jobs {
		if s.readers > 0 {
			continue
		}
		if s.deleted || (!s.ExpiresAt.IsZero() && !s.ExpiresAt.After(now) && s.State != "snapshot" && s.State != "packing") {
			if os.Remove(filepath.Join(m.root, id+".age")) != nil {
				if _, e := os.Lstat(filepath.Join(m.root, id+".age")); !os.IsNotExist(e) {
					continue
				}
			}
			os.Remove(filepath.Join(m.root, id+".json"))
			delete(m.jobs, id)
			for req, jid := range m.requests {
				if jid == id {
					delete(m.requests, req)
				}
			}
		}
	}
}
func (m *Manager) Start(ctx context.Context, kind Kind, password []byte, requestID string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	if kind != Config && kind != Full || len(password) == 0 || len(password) > 1024 || len(requestID) == 0 || len(requestID) > 128 {
		return Job{}, errors.New("invalid backup request")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())
	if id, ok := m.requests[requestID]; ok {
		if s := m.jobs[id]; s != nil && !s.deleted {
			return s.Job, nil
		}
	}
	if m.closed || m.busy {
		return Job{}, ErrBusy
	}
	ready := 0
	for _, s := range m.jobs {
		if s.State == "ready" {
			ready++
		}
	}
	if ready >= 3 || len(m.jobs) >= 32 {
		return Job{}, ErrBusy
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Job{}, err
	}
	now := time.Now().UTC()
	j := Job{ID: hex.EncodeToString(id[:]), State: "snapshot", Kind: kind, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), ServerVersion: m.coordinator.version}
	if err := m.saveLocked(j); err != nil {
		return Job{}, err
	}
	m.jobs[j.ID] = &jobState{Job: j}
	m.requests[requestID] = j.ID
	m.busy = true
	secret := append([]byte(nil), password...)
	m.wg.Add(1)
	go m.run(j.ID, secret)
	return j, nil
}
func (m *Manager) run(id string, password []byte) {
	defer m.wg.Done()
	defer clear(password)
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Minute)
	defer cancel()
	finish := func(size int64, sum string, err error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		s := m.jobs[id]
		s.SizeBytes = size
		s.SHA256 = sum
		s.State = "ready"
		if err != nil {
			s.State = "error"
			s.ErrorCode = "creation_failed"
			s.SizeBytes = 0
			s.SHA256 = ""
			os.Remove(filepath.Join(m.root, id+".age"))
		}
		if e := m.saveLocked(s.Job); e != nil {
			s.State = "error"
			s.ErrorCode = "metadata_failed"
			os.Remove(filepath.Join(m.root, id+".age"))
		}
		m.busy = false
	}
	m.mu.Lock()
	kind := m.jobs[id].Kind
	m.mu.Unlock()
	snapshot, err := m.coordinator.Capture(ctx, kind)
	if err != nil {
		finish(0, "", err)
		return
	}
	defer snapshot.Release()
	var size int64
	for _, e := range snapshot.Manifest.Entries {
		size += e.Size
	}
	m.mu.Lock()
	used := int64(0)
	for _, s := range m.jobs {
		used += s.SizeBytes
	}
	m.jobs[id].State = "packing"
	m.mu.Unlock()
	limit := m.quota - used - size
	if limit <= 0 {
		finish(0, "", errors.New("backup quota exceeded"))
		return
	}
	f, err := os.CreateTemp(m.root, ".partial-archive-")
	if err != nil {
		finish(0, "", err)
		return
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	bounded := &quotaWriter{w: io.MultiWriter(f, h), remaining: limit}
	err = Write(ctx, bounded, *snapshot, password)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(m.root, id+".age"))
	}
	finish(limit-bounded.remaining, hex.EncodeToString(h.Sum(nil)), err)
}

type quotaWriter struct {
	w         io.Writer
	remaining int64
}

func (w *quotaWriter) Write(b []byte) (int, error) {
	if int64(len(b)) > w.remaining {
		return 0, errors.New("backup quota exceeded")
	}
	n, e := w.w.Write(b)
	w.remaining -= int64(n)
	return n, e
}
func (m *Manager) Get(id string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())
	s := m.jobs[id]
	if s == nil || s.deleted {
		return Job{}, ErrNotFound
	}
	return s.Job, nil
}
func (m *Manager) List() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())
	out := []Job{}
	for _, s := range m.jobs {
		if !s.deleted {
			out = append(out, s.Job)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}
func (m *Manager) Open(id string) (io.ReadCloser, Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.jobs[id]
	if s == nil || s.deleted || s.State != "ready" || !s.ExpiresAt.After(time.Now()) {
		return nil, Job{}, ErrNotFound
	}
	name := filepath.Join(m.root, id+".age")
	info, e := os.Lstat(name)
	if e != nil || !info.Mode().IsRegular() || info.Size() != s.SizeBytes {
		return nil, Job{}, ErrNotFound
	}
	f, e := os.Open(name)
	if e != nil {
		return nil, Job{}, e
	}
	s.readers++
	return &leasedReader{ReadCloser: f, close: func() { m.mu.Lock(); defer m.mu.Unlock(); s.readers--; m.cleanupLocked(time.Now()) }}, s.Job, nil
}

type leasedReader struct {
	io.ReadCloser
	once  sync.Once
	close func()
}

func (r *leasedReader) Close() (err error) {
	r.once.Do(func() { err = r.ReadCloser.Close(); r.close() })
	return
}
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.jobs[id]
	if s == nil {
		return ErrNotFound
	}
	if s.State == "snapshot" || s.State == "packing" {
		return ErrBusy
	}
	s.deleted = true
	m.cleanupLocked(time.Now())
	return nil
}
