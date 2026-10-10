package vlessserver

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	serverbackup "quiclab/internal/backup"
	"quiclab/internal/vless"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Device struct {
	ID       string    `json:"id"`
	UUID     string    `json:"uuid"`
	Expires  time.Time `json:"expires"`
	Disabled bool      `json:"disabled"`
}
type Snapshot struct {
	Revision uint64   `json:"revision"`
	Config   Config   `json:"config"`
	Devices  []Device `json:"devices"`
}
type workerRecord struct {
	PinnedDemux string
	Desired     Snapshot        `json:"desired"`
	Revoked     map[string]bool `json:"revoked"`
}
type managedServer interface {
	SetClients(context.Context, []vless.ServerClient) error
	Revoke(string)
	Close() error
}
type AdmissionFunc func(context.Context, string, string) (time.Duration, error)
type Worker struct {
	resolver            ipResolver
	mu                  sync.Mutex
	dir                 string
	ctx                 context.Context
	cancel              context.CancelFunc
	stop                func() bool
	record              workerRecord
	applied             uint64
	live                managedServer
	liveConfig          Config
	closed, writeFailed bool
	auth                atomic.Pointer[Snapshot]
	admission           AdmissionFunc
	start               func(context.Context, vless.ServerOptions) (managedServer, error)
	persist             func(workerRecord) error
}

var errApply = errors.New("VLESS application not confirmed")
var errRevision = errors.New("VLESS revision rejected")

func normalizeSnapshot(s Snapshot) (Snapshot, error) {
	raw, e := json.Marshal(s)
	if e != nil || len(raw) > 128*1024 {
		return Snapshot{}, errConfig
	}
	if json.Unmarshal(raw, &s) != nil {
		return Snapshot{}, errConfig
	}
	if s.Revision == 0 || len(s.Devices) > vless.MaxServerClients || s.Config.Validate() != nil {
		return Snapshot{}, errConfig
	}
	ids, keys := map[string]bool{}, map[string]bool{}
	for i := range s.Devices {
		d := &s.Devices[i]
		d.UUID = strings.ToLower(d.UUID)
		if d.ID == "" || len(d.ID) > 128 || strings.ContainsAny(d.ID, "\x00\r\n \t") || ids[d.ID] || keys[d.UUID] || d.Expires.IsZero() {
			return Snapshot{}, errConfig
		}
		if len(d.UUID) != 36 || d.UUID[8] != '-' || d.UUID[13] != '-' || d.UUID[18] != '-' || d.UUID[23] != '-' {
			return Snapshot{}, errConfig
		}
		if b, e := hex.DecodeString(strings.ReplaceAll(d.UUID, "-", "")); e != nil || len(b) != 16 {
			return Snapshot{}, errConfig
		}
		ids[d.ID] = true
		keys[d.UUID] = true
	}
	return s, nil
}
func OpenWorker(ctx context.Context, dir string, admission AdmissionFunc) (*Worker, error) {
	if admission == nil || privateDirectory(dir) != nil {
		return nil, errConfig
	}
	child, cancel := context.WithCancel(ctx)
	w := &Worker{resolver: net.DefaultResolver, dir: dir, ctx: child, cancel: cancel, admission: admission, record: workerRecord{Revoked: map[string]bool{}}}
	w.start = func(ctx context.Context, o vless.ServerOptions) (managedServer, error) {
		return vless.StartServer(ctx, o)
	}
	w.persist = w.save
	path := filepath.Join(dir, "vless-state.json")
	if _, e := os.Lstat(path); e == nil {
		raw, e := readMaterial(path, true)
		if e != nil {
			cancel()
			return nil, errConfig
		}
		if json.Unmarshal(raw, &w.record) != nil || w.record.Revoked == nil {
			cancel()
			return nil, errConfig
		}
		normalized, e := normalizeSnapshot(w.record.Desired)
		if e != nil {
			cancel()
			return nil, e
		}
		w.record.Desired = normalized
	} else if !os.IsNotExist(e) {
		cancel()
		return nil, errConfig
	}
	// No listener is restored from disk alone. A fresh backend Apply and current
	// device admission are required after every worker restart.
	w.mu.Lock()
	w.stop = context.AfterFunc(ctx, func() { w.Close() })
	w.mu.Unlock()
	return w, nil
}
func (w *Worker) save(r workerRecord) error {
	if privateDirectory(w.dir) != nil {
		return errApply
	}
	raw, e := json.Marshal(r)
	if e != nil || len(raw) > 1024*1024 {
		return errApply
	}
	f, e := os.CreateTemp(w.dir, ".vless-state-*")
	if e != nil {
		return errApply
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return errApply
	}
	if e = serverbackup.Replace(tmp, filepath.Join(w.dir, "vless-state.json")); e != nil {
		return errApply
	}
	dir, e := os.Open(w.dir)
	if e != nil {
		return errApply
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return errApply
	}
	return nil
}
func (w *Worker) authorize(ctx context.Context, uuid string) (time.Duration, error) {
	s := w.auth.Load()
	if s == nil {
		return 0, errApply
	}
	for _, d := range s.Devices {
		if d.UUID != uuid {
			continue
		}
		if d.Disabled || !d.Expires.After(time.Now()) {
			return 0, errApply
		}
		ttl, e := w.admission(ctx, d.ID, d.UUID)
		if e != nil || ttl <= 0 || ttl > 2*time.Second {
			return 0, errApply
		}
		return min(ttl, time.Until(d.Expires)), nil
	}
	return 0, errApply
}
func (w *Worker) Apply(ctx context.Context, input Snapshot) error {
	s, e := normalizeSnapshot(input)
	if e != nil {
		return e
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.writeFailed || w.ctx.Err() != nil || ctx.Err() != nil {
		return errApply
	}
	current := w.record.Desired
	if s.Revision < current.Revision {
		return errRevision
	}
	if s.Revision == current.Revision {
		a, _ := json.Marshal(s)
		b, _ := json.Marshal(current)
		if !bytes.Equal(a, b) {
			return errRevision
		}
		if w.applied == s.Revision && w.live != nil {
			return nil
		}
	}
	revoked := map[string]bool{}
	for id, v := range w.record.Revoked {
		revoked[id] = v
	}
	enabled := map[string]bool{}
	now := time.Now()
	for _, d := range s.Devices {
		if !d.Disabled && d.Expires.After(now) {
			if revoked[d.UUID] {
				return errRevision
			}
			enabled[d.UUID] = true
		} else {
			revoked[d.UUID] = true
		}
	}
	for _, d := range current.Devices {
		if !enabled[d.UUID] {
			revoked[d.UUID] = true
		}
	}
	next := workerRecord{Desired: s, Revoked: revoked}
	if s.Config.Mode == "demux-only" && current.Config.Mode == "demux-only" && s.Config.DemuxEndpoint == current.Config.DemuxEndpoint {
		next.PinnedDemux = w.record.PinnedDemux
	}
	if e = w.persist(next); e != nil {
		w.auth.Store(nil)
		w.writeFailed = true
		if w.live != nil {
			w.live.Close()
			w.live = nil
		}
		return errApply
	}
	w.record = next
	w.auth.Store(&s)
	if w.live != nil {
		for id := range revoked {
			w.live.Revoke(id)
		}
	}
	var clients []vless.ServerClient
	for _, d := range s.Devices {
		if enabled[d.UUID] {
			clients = append(clients, vless.ServerClient{UUID: d.UUID, Flow: s.Config.Flow})
		}
	}
	if w.live != nil && reflect.DeepEqual(s.Config, w.liveConfig) {
		if w.live.SetClients(ctx, clients) != nil {
			w.live.Close()
			w.live = nil
			return errApply
		}
	} else {
		if w.live != nil {
			w.live.Close()
			w.live = nil
		}
		runtimeConfig := s.Config
		if next.PinnedDemux != "" {
			runtimeConfig.DemuxEndpoint = next.PinnedDemux
		}
		o, e := runtimeConfig.serverOptions(ctx, clients, w.resolver)
		if e != nil {
			return errApply
		}
		if s.Config.Mode == "demux-only" && next.PinnedDemux == "" {
			next.PinnedDemux = o.DemuxEndpoint
			if e = w.persist(next); e != nil {
				w.auth.Store(nil)
				w.writeFailed = true
				return errApply
			}
			w.record = next
		}
		o.Admission = w.authorize
		w.live, e = w.start(w.ctx, o)
		if e != nil {
			w.live = nil
			return errApply
		}
		w.liveConfig = s.Config
	}
	w.applied = s.Revision
	return nil
}
func (w *Worker) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	w.cancel()
	if w.stop != nil {
		w.stop()
	}
	w.auth.Store(nil)
	if w.live != nil {
		return w.live.Close()
	}
	return nil
}
