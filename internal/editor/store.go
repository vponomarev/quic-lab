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
	"regexp"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
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
	Inventory     json.RawMessage `json:"inventory,omitempty"`
	ID            string          `json:"id"`
	Paired        bool            `json:"paired"`
	ExpiresAt     time.Time       `json:"expiresAt"`
	Generation    int64           `json:"generation"`
	Configuration json.RawMessage `json:"configuration,omitempty"`
	Pending       *Candidate      `json:"pending,omitempty"`
	Status        string          `json:"status"`
}
type session struct {
	expiry                             *time.Timer
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
	audit     *Audit
	realClock bool
	mu        sync.Mutex
	now       func() time.Time
	sessions  map[string]*session
}

func NewStore(now func() time.Time) *Store {
	realClock := now == nil
	if now == nil {
		now = time.Now
	}
	return &Store{realClock: realClock, now: now, sessions: map[string]*session{}}
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
			_ = s.record(id, "expired", x.revision)
			s.drop(id)
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
	if e := s.record(id, "created", 0); e != nil {
		return Ticket{}, e
	}
	until := s.now().Add(5 * time.Minute)
	s.sessions[id] = &session{state: State{ID: id, ExpiresAt: until, Status: "waiting"}, owner: owner, pair: sha256.Sum256([]byte(pair)), browser: sha256.Sum256([]byte(browser)), duration: duration}
	s.scheduleExpiry(id)
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
func (s *Store) Pair(id, secret string, generation int64, configuration json.RawMessage, inventory ...json.RawMessage) (Grant, error) {
	var inv json.RawMessage
	if len(inventory) > 1 {
		return Grant{}, ErrInvalid
	}
	if len(inventory) == 1 {
		inv = inventory[0]
		if len(inv) > 0 && !validInventory(inv) {
			return Grant{}, ErrInvalid
		}
	}
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
	if e := s.record(id, "paired", 0); e != nil {
		return Grant{}, e
	}
	x.device = sha256.Sum256([]byte(credential))
	x.pair = [32]byte{}
	x.state.Paired = true
	x.state.ExpiresAt = s.now().Add(x.duration)
	x.state.Generation = generation
	x.state.Configuration = copyRaw(configuration)
	x.state.Status = "ready"
	x.state.Inventory = copyRaw(inv)
	s.scheduleExpiry(id)
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
	out.Inventory = copyRaw(out.Inventory)
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
	if e := s.record(id, "submitted", x.revision+1); e != nil {
		return 0, e
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
	if e := s.record(id, status, revision); e != nil {
		return e
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
	if e := s.record(id, "local_edit", 0); e != nil {
		return e
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
	_ = s.record(id, "revoked_browser", 0)
	s.drop(id)
	return nil
}
func (s *Store) RevokeDevice(id, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e := s.deviceSession(id, secret); e != nil {
		return e
	}
	_ = s.record(id, "revoked_device", 0)
	s.drop(id)
	return nil
}

var appPackage = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)+$`)

func validInventory(raw json.RawMessage) bool {
	if len(raw) > 512<<10 {
		return false
	}
	var inv struct {
		Complete bool `json:"complete"`
		Entries  []struct {
			PackageID string `json:"packageId"`
			Label     string `json:"label"`
		} `json:"entries"`
	}
	if !strictJSON(raw, &inv) || len(inv.Entries) > 2048 {
		return false
	}
	seen := map[string]bool{}
	for _, a := range inv.Entries {
		if len(a.PackageID) > 256 || !appPackage.MatchString(a.PackageID) || seen[a.PackageID] || utf8.RuneCountInString(a.Label) > 256 || a.Label == "" {
			return false
		}
		for _, r := range a.Label {
			if unicode.IsControl(r) {
				return false
			}
		}
		seen[a.PackageID] = true
	}
	return true
}
func (s *Store) RevokeOwner(owner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, x := range s.sessions {
		if x.owner == owner {
			_ = s.record(id, "logout", x.revision)
			s.drop(id)
		}
	}
}

// Called under mu. Real-clock sessions erase payloads even if every peer disappears.
func (s *Store) scheduleExpiry(id string) {
	if !s.realClock {
		return
	}
	x := s.sessions[id]
	if x == nil {
		return
	}
	if x.expiry != nil {
		x.expiry.Stop()
	}
	x.expiry = time.AfterFunc(time.Until(x.state.ExpiresAt), func() { s.mu.Lock(); defer s.mu.Unlock(); s.prune() })
}
func (s *Store) drop(id string) {
	if x := s.sessions[id]; x != nil && x.expiry != nil {
		x.expiry.Stop()
	}
	delete(s.sessions, id)
}
