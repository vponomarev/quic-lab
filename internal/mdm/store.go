package mdm

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type invitationState struct {
	Expires                   time.Time
	Rights                    Rights
	BindingID, RegistrationID string
}
type auditEntry struct {
	Event      Event
	ReceivedAt time.Time
}
type bindingState struct {
	Binding    Binding
	SecretHash string
	Desired    *ConfigRevision
	Commands   []Command
	Audit      []auditEntry
	Seen       map[string]time.Time
}
type snapshot struct {
	Version     int
	Invitations map[string]invitationState
	Bindings    map[string]bindingState
}
type Store struct {
	mu      sync.Mutex
	dir     string
	state   snapshot
	changed chan struct{}
	failed  bool
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, changed: make(chan struct{}), state: snapshot{Version: 1, Invitations: map[string]invitationState{}, Bindings: map[string]bindingState{}}}
	path := filepath.Join(dir, "state.json")
	if info, e := os.Stat(path); e == nil && info.Size() > 64<<20 {
		return nil, errors.New("MDM state exceeds size limit")
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &s.state); err != nil {
		return nil, fmt.Errorf("MDM state: %w", err)
	}
	if s.state.Version != 1 || s.state.Invitations == nil || s.state.Bindings == nil {
		return nil, errors.New("invalid MDM state")
	}
	for id, b := range s.state.Bindings {
		if b.Binding.ID != id || b.Binding.Epoch < 0 || len(b.SecretHash) != 64 || b.Seen == nil {
			return nil, errors.New("invalid binding state")
		}
	}
	return s, nil
}
func randomID() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func validSecret(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}
func (s *Store) clone() snapshot {
	raw, _ := json.Marshal(s.state)
	var n snapshot
	_ = json.Unmarshal(raw, &n)
	return n
}
func (s *Store) save(n snapshot) error {
	if s.failed {
		return errors.New("MDM store requires reopen after failed durability check")
	}
	raw, e := json.Marshal(n)
	if e != nil {
		return e
	}
	if len(raw) > 64<<20 {
		return ErrLimit
	}
	f, e := os.CreateTemp(s.dir, ".state-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
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
		return e
	}
	if e = os.Rename(name, filepath.Join(s.dir, "state.json")); e != nil {
		return e
	}
	d, e := os.Open(s.dir)
	if e == nil {
		e = d.Sync()
		_ = d.Close()
	}
	if e != nil {
		s.failed = true
		return e
	}
	s.state = n
	close(s.changed)
	s.changed = make(chan struct{})
	return nil
}
func (s *Store) Authenticate(id, secret string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Bindings[id]
	return !s.failed && ok && validSecret(secret) && subtle.ConstantTimeCompare([]byte(b.SecretHash), []byte(digest(secret))) == 1
}
func (s *Store) Audit(id string) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Bindings[id]
	if !ok {
		return nil, ErrUnauthorized
	}
	out := make([]Event, 0, len(b.Audit))
	for _, v := range b.Audit {
		out = append(out, v.Event)
	}
	return out, nil
}
func (s *Store) Prune(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.clone()
	for k, v := range n.Invitations {
		if now.Sub(v.Expires) > 90*24*time.Hour {
			delete(n.Invitations, k)
		}
	}
	for id, b := range n.Bindings {
		kept := b.Audit[:0]
		for _, v := range b.Audit {
			if now.Sub(v.ReceivedAt) <= 90*24*time.Hour {
				kept = append(kept, v)
			}
		}
		b.Audit = kept
		for k, v := range b.Seen {
			if now.Sub(v) > 90*24*time.Hour {
				delete(b.Seen, k)
			}
		}
		b.Commands = liveCommands(b.Commands, now)
		n.Bindings[id] = b
	}
	return s.save(n)
}
func liveCommands(in []Command, now time.Time) []Command {
	out := make([]Command, 0, len(in))
	for _, c := range in {
		if now.Before(c.ExpiresAt) {
			out = append(out, c)
		}
	}
	return out
}
