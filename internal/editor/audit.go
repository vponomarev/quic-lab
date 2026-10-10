package editor

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// Audit has no fields capable of carrying configuration, credentials or app lists.
type AuditEvent struct {
	At       time.Time `json:"at"`
	Session  string    `json:"session"`
	Action   string    `json:"action"`
	Revision int64     `json:"revision,omitempty"`
}
type Audit struct {
	mu  sync.Mutex
	dir string
	now func() time.Time
}

func NewAudit(dir string, now func() time.Time) *Audit {
	if now == nil {
		now = time.Now
	}
	return &Audit{dir: dir, now: now}
}

var auditID = regexp.MustCompile(`^[0-9a-f]{64}$`)
var auditDay = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.jsonl$`)

func (a *Audit) prune() error {
	if e := os.MkdirAll(a.dir, 0700); e != nil {
		return e
	}
	files, e := os.ReadDir(a.dir)
	if e != nil {
		return e
	}
	cutoff := a.now().UTC().AddDate(0, 0, -90).Format("2006-01-02")
	for _, f := range files {
		if !f.IsDir() && auditDay.MatchString(f.Name()) && f.Name()[:10] < cutoff {
			if e = os.Remove(filepath.Join(a.dir, f.Name())); e != nil {
				return e
			}
		}
	}
	return nil
}
func (a *Audit) Append(v AuditEvent) error {
	if !auditID.MatchString(v.Session) || v.Revision < 0 {
		return ErrInvalid
	}
	switch v.Action {
	case "created", "paired", "submitted", "applied", "error", "local_edit", "revoked_browser", "revoked_device", "logout", "expired":
	default:
		return ErrInvalid
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.prune(); e != nil {
		return e
	}
	v.At = a.now().UTC()
	path := filepath.Join(a.dir, v.At.Format("2006-01-02")+".jsonl")
	old, e := readAuditFile(path)
	if e != nil {
		return e
	}
	// Recover only an incomplete final record left by an older append writer.
	if len(old) > 0 && old[len(old)-1] != '\n' {
		old = old[:bytes.LastIndexByte(old, '\n')+1]
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return e
	}
	raw = append(raw, '\n')
	if len(old)+len(raw) > 8<<20 {
		return errors.New("editor audit daily limit")
	}
	f, e := os.CreateTemp(a.dir, ".audit-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(append(old, raw...)); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	// Rename publishes the complete record exactly once; failed writes leave the old file intact.
	return os.Rename(f.Name(), path)
}
func readAuditFile(path string) ([]byte, error) {
	f, e := os.Open(path)
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if e != nil {
		return nil, e
	}
	if len(raw) > 8<<20 {
		return nil, ErrInvalid
	}
	return raw, nil
}

// Latest 500 matching events for a selected UTC day. File reads do not hold
// the writer mutex, so browsing history cannot block revocation.
func (a *Audit) Read(id string, days ...string) ([]AuditEvent, error) {
	if id != "" && !auditID.MatchString(id) {
		return nil, ErrInvalid
	}
	day := a.now().UTC().Format("2006-01-02")
	if len(days) > 0 && days[0] != "" {
		day = days[0]
	}
	parsed, e := time.Parse("2006-01-02", day)
	if e != nil || parsed.Format("2006-01-02") != day {
		return nil, ErrInvalid
	}
	a.mu.Lock()
	e = a.prune()
	a.mu.Unlock()
	if e != nil {
		return nil, e
	}
	out := []AuditEvent{}
	if day < a.now().UTC().AddDate(0, 0, -90).Format("2006-01-02") {
		return out, nil
	}
	raw, e := readAuditFile(filepath.Join(a.dir, day+".jsonl"))
	if e != nil {
		return nil, e
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 1024), 4096)
	for scanner.Scan() {
		var v AuditEvent
		if json.Unmarshal(scanner.Bytes(), &v) != nil {
			return nil, ErrInvalid
		}
		if id == "" || v.Session == id {
			out = append(out, v)
			if len(out) > 500 {
				copy(out, out[1:])
				out = out[:500]
			}
		}
	}
	return out, scanner.Err()
}
func (s *Store) EnableAudit(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit = NewAudit(dir, s.now)
}
func (s *Store) AuditEvents(id string, days ...string) ([]AuditEvent, error) {
	s.mu.Lock()
	a := s.audit
	s.mu.Unlock()
	if a == nil {
		return []AuditEvent{}, nil
	}
	return a.Read(id, days...)
}

var ErrAudit = errors.New("editor audit unavailable")

func (s *Store) record(id, action string, revision int64) error {
	if s.audit == nil {
		return nil
	}
	if s.audit.Append(AuditEvent{Session: id, Action: action, Revision: revision}) != nil {
		slog.Error("editor audit unavailable", "session", id, "action", action)
		return ErrAudit
	}
	return nil
}
