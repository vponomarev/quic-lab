// Package editor holds short-lived, consented configuration editing sessions.
// It never persists credentials, configuration or enrollment across server restart.
package editor

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"quiclab/internal/mdm"
	"sync"
	"time"
)

var (
	ErrUnauthorized = errors.New("editor access unavailable")
	ErrInvalid      = errors.New("invalid editor request")
	ErrConflict     = errors.New("editor configuration conflict")
	ErrLimit        = errors.New("editor session limit")
)

type Ticket struct {
	ID           string    `json:"id"`
	PairToken    string    `json:"pairToken"`
	BrowserToken string    `json:"browserToken"`
	PairUntil    time.Time `json:"pairUntil"`
}
type Grant struct {
	ID          string    `json:"id"`
	DeviceToken string    `json:"deviceToken"`
	ExpiresAt   time.Time `json:"expiresAt"`
}
type Candidate struct {
	Revision       int64           `json:"revision"`
	BaseGeneration int64           `json:"baseGeneration"`
	Document       json.RawMessage `json:"document,omitempty"`
}
type State struct {
	ID            string          `json:"id"`
	Paired        bool            `json:"paired"`
	ExpiresAt     time.Time       `json:"expiresAt"`
	Generation    int64           `json:"generation"`
	Configuration json.RawMessage `json:"configuration,omitempty"`
	Pending       *Candidate      `json:"pending,omitempty"`
	Status        string          `json:"status"`
}
type session struct {
	state                              State
	owner                              string
	pair, browser, device              [32]byte
	duration                           time.Duration
	revision                           int64
	lastAckRevision, lastAckGeneration int64
	lastAckStatus                      string
	lastAckHash                        [32]byte
}
type Store struct {
	mu       sync.Mutex
	now      func() time.Time
	sessions map[string]*session
}

func NewStore(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{now: now, sessions: map[string]*session{}}
}
func token() (string, error) {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(b[:]), nil
}
func matches(hash [32]byte, raw string) bool {
	if len(raw) != 64 {
		return false
	}
	h := sha256.Sum256([]byte(raw))
	return subtle.ConstantTimeCompare(hash[:], h[:]) == 1
}
func (s *Store) prune() {
	now := s.now()
	for id, x := range s.sessions {
		if !now.Before(x.state.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}
func (s *Store) Create(owner string, duration time.Duration) (Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	if owner == "" || len(owner) > 256 || (duration != 15*time.Minute && duration != 30*time.Minute && duration != time.Hour) {
		return Ticket{}, ErrInvalid
	}
	if len(s.sessions) >= 100 {
		return Ticket{}, ErrLimit
	}
	id, e := token()
	if e != nil {
		return Ticket{}, e
	}
	pair, e := token()
	if e != nil {
		return Ticket{}, e
	}
	browser, e := token()
	if e != nil {
		return Ticket{}, e
	}
	until := s.now().Add(5 * time.Minute)
	s.sessions[id] = &session{state: State{ID: id, ExpiresAt: until, Status: "waiting"}, owner: owner, pair: sha256.Sum256([]byte(pair)), browser: sha256.Sum256([]byte(browser)), duration: duration}
	return Ticket{id, pair, browser, until}, nil
}
func reportValid(raw json.RawMessage) bool {
	if mdm.ValidateDocument(raw) != nil {
		return false
	}
	var d mdm.ConfigurationDocument
	_ = json.Unmarshal(raw, &d)
	for _, p := range d.Profiles {
		if p.Identity != nil || p.RemoveIdentity {
			return false
		}
	}
	return true
}
func copyRaw(raw json.RawMessage) json.RawMessage { return append(json.RawMessage(nil), raw...) }
func (s *Store) Pair(id, secret string, generation int64, configuration json.RawMessage) (Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	x := s.sessions[id]
	if x == nil || x.state.Paired || !matches(x.pair, secret) {
		return Grant{}, ErrUnauthorized
	}
	if generation < 0 || !reportValid(configuration) {
		return Grant{}, ErrInvalid
	}
	credential, e := token()
	if e != nil {
		return Grant{}, e
	}
	x.device = sha256.Sum256([]byte(credential))
	x.pair = [32]byte{}
	x.state.Paired = true
	x.state.ExpiresAt = s.now().Add(x.duration)
	x.state.Generation = generation
	x.state.Configuration = copyRaw(configuration)
	x.state.Status = "ready"
	return Grant{id, credential, x.state.ExpiresAt}, nil
}
func (s *Store) browserSession(id, owner, secret string) (*session, error) {
	s.prune()
	x := s.sessions[id]
	if x == nil || x.owner != owner || !matches(x.browser, secret) {
		return nil, ErrUnauthorized
	}
	return x, nil
}
func (s *Store) deviceSession(id, secret string) (*session, error) {
	s.prune()
	x := s.sessions[id]
	if x == nil || !x.state.Paired || !matches(x.device, secret) {
		return nil, ErrUnauthorized
	}
	return x, nil
}
func clone(x *session, device bool) State {
	out := x.state
	out.Configuration = copyRaw(out.Configuration)
	if out.Pending != nil {
		c := *out.Pending
		out.Pending = &c
		if device {
			c.Document = copyRaw(c.Document)
		} else {
			c.Document = nil
		}
	}
	return out
}
func (s *Store) Read(id, owner, secret string) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, e := s.browserSession(id, owner, secret)
	if e != nil {
		return State{}, e
	}
	return clone(x, false), nil
}
func (s *Store) Poll(id, secret string) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, e := s.deviceSession(id, secret)
	if e != nil {
		return State{}, e
	}
	return clone(x, true), nil
}
func (s *Store) Submit(id, owner, secret string, base int64, document json.RawMessage) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, e := s.browserSession(id, owner, secret)
	if e != nil {
		return 0, e
	}
	if !x.state.Paired || x.state.Pending != nil || x.state.Generation != base {
		return 0, ErrConflict
	}
	if mdm.ValidateDocument(document) != nil {
		return 0, ErrInvalid
	}
	x.revision++
	x.state.Pending = &Candidate{x.revision, base, copyRaw(document)}
	x.state.Status = "pending"
	return x.revision, nil
}

// Acknowledge carries the phone's sanitized actual configuration, not an assumption
// that the last submitted candidate was applied. Errors never include payloads.
func (s *Store) Acknowledge(id, secret string, revision, generation int64, configuration json.RawMessage, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, e := s.deviceSession(id, secret)
	if e != nil {
		return e
	}
	if revision > 0 && revision == x.lastAckRevision && generation == x.lastAckGeneration && status == x.lastAckStatus && sha256.Sum256(configuration) == x.lastAckHash {
		return nil
	}
	p := x.state.Pending
	if p == nil || p.Revision != revision || generation < x.state.Generation {
		return ErrConflict
	}
	if status != "applied" && status != "error" {
		return ErrInvalid
	}
	if status == "applied" && generation <= p.BaseGeneration {
		return ErrConflict
	}
	if !reportValid(configuration) {
		return ErrInvalid
	}
	x.state.Configuration = copyRaw(configuration)
	x.state.Generation = generation
	x.state.Pending = nil
	x.state.Status = status
	x.lastAckRevision, x.lastAckGeneration, x.lastAckStatus, x.lastAckHash = revision, generation, status, sha256.Sum256(configuration)
	return nil
}
func (s *Store) Report(id, secret string, generation int64, configuration json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, e := s.deviceSession(id, secret)
	if e != nil {
		return e
	}
	if generation < x.state.Generation {
		return ErrConflict
	}
	if !reportValid(configuration) {
		return ErrInvalid
	}
	if generation == x.state.Generation {
		if sha256.Sum256(configuration) != sha256.Sum256(x.state.Configuration) {
			return ErrConflict
		}
		return nil
	}
	x.state.Generation = generation
	x.state.Configuration = copyRaw(configuration)
	// A local phone edit invalidates a draft pending for the previous generation.
	x.state.Pending = nil
	x.state.Status = "ready"
	return nil
}
func (s *Store) Revoke(id, owner, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e := s.browserSession(id, owner, secret); e != nil {
		return e
	}
	delete(s.sessions, id)
	return nil
}
func (s *Store) RevokeDevice(id, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e := s.deviceSession(id, secret); e != nil {
		return e
	}
	delete(s.sessions, id)
	return nil
}
