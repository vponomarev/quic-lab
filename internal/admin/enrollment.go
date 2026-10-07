package admin

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"time"
)

const DefaultEnrollmentTTL = 24 * time.Hour
const DefaultEnrollmentLimit = 5

type Enrollment struct {
	ID          string            `json:"id"`
	UserID      string            `json:"user_id"`
	Expires     time.Time         `json:"expires"`
	MaxDevices  int               `json:"max_devices"`
	Used        int               `json:"used"`
	Revoked     bool              `json:"revoked,omitempty"`
	CanDisplay  bool              `json:"-"`
	TokenCipher string            `json:"token_cipher,omitempty"`
	TokenHash   string            `json:"token_hash,omitempty"`
	Requests    map[string]string `json:"requests,omitempty"`
}

func enrollmentHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func enrollmentMetadata(en Enrollment) Enrollment {
	en.CanDisplay = en.TokenCipher != "" && !en.Revoked && en.Expires.After(time.Now()) && en.Used < en.MaxDevices
	en.TokenCipher = ""
	en.TokenHash = ""
	en.Requests = nil
	return en
}
func (s *Store) NewEnrollment(userID string, ttl time.Duration, maxDevices int) (Enrollment, string, error) {
	if ttl == 0 {
		ttl = DefaultEnrollmentTTL
	}
	if maxDevices == 0 {
		maxDevices = DefaultEnrollmentLimit
	}
	if ttl <= 0 || ttl > 30*24*time.Hour || maxDevices < 1 || maxDevices > 100 {
		return Enrollment{}, "", errors.New("invalid enrollment limits")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.state.Users[userID]
	now := time.Now().UTC()
	if !ok || u.Disabled || !u.Expires.After(now) {
		return Enrollment{}, "", errors.New("user unavailable")
	}
	var secret [32]byte
	if _, e := rand.Read(secret[:]); e != nil {
		return Enrollment{}, "", e
	}
	token := hex.EncodeToString(secret[:])
	id, e := newDeviceID()
	if e != nil {
		return Enrollment{}, "", e
	}
	en := Enrollment{ID: id, UserID: userID, Expires: now.Add(ttl), MaxDevices: maxDevices, TokenHash: enrollmentHash(token), Requests: map[string]string{}}
	en.TokenCipher, e = s.sealEnrollmentToken(en.ID, token)
	if e != nil {
		return Enrollment{}, "", e
	}
	if en.Expires.After(u.Expires) {
		en.Expires = u.Expires
	}
	if s.state.Enrollments == nil {
		s.state.Enrollments = map[string]Enrollment{}
	}
	s.state.Enrollments[id] = en
	if e := s.save(); e != nil {
		if !statePublished(e) {
			delete(s.state.Enrollments, id)
		}
		return Enrollment{}, "", e
	}
	return enrollmentMetadata(en), token, nil
}
func (s *Store) Enroll(token, requestID, deviceName string) (Device, error) {
	d, _, e := s.EnrollWithUpdate(token, requestID, deviceName)
	return d, e
}
func (s *Store) EnrollWithUpdate(token, requestID, deviceName string) (Device, string, error) {
	if len(token) != 64 || len(requestID) < 1 || len(requestID) > 128 {
		return Device{}, "", errors.New("invalid enrollment")
	}
	if _, e := hex.DecodeString(token); e != nil {
		return Device{}, "", errors.New("invalid enrollment")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := enrollmentHash(token)
	now := time.Now().UTC()
	var en Enrollment
	found := false
	for _, candidate := range s.state.Enrollments {
		if candidate.TokenHash == hash {
			en = candidate
			found = true
			break
		}
	}
	u, ok := s.state.Users[en.UserID]
	if !found || en.Revoked || !en.Expires.After(now) || !ok || u.Disabled || !u.Expires.After(now) {
		return Device{}, "", errors.New("enrollment unavailable")
	}
	if id := en.Requests[requestID]; id != "" {
		d, ok := s.state.Devices[id]
		if !ok || d.Disabled || !d.Expires.After(now) {
			return Device{}, "", errors.New("device unavailable")
		}
		updateToken, e := replayUpdateToken(d, en, token, requestID)
		if e != nil {
			return Device{}, "", e
		}
		// A prior primary rename may have succeeded without a successful directory
		// sync. Recover confirmed durability before acknowledging a valid replay.
		if e := s.save(); e != nil {
			return Device{}, "", e
		}
		return d, updateToken, nil
	}
	if en.Used >= en.MaxDevices {
		return Device{}, "", errors.New("enrollment device limit")
	}
	d, e := s.createDeviceLocked(en.UserID, deviceName, false)
	if e != nil {
		return Device{}, "", e
	}
	updateToken, e := issueUpdateToken(&d, en, token, requestID)
	if e != nil {
		delete(s.state.Devices, d.ID)
		return Device{}, "", e
	}
	s.state.Devices[d.ID] = d
	previous := en
	en.Requests = map[string]string{}
	for k, v := range previous.Requests {
		en.Requests[k] = v
	}
	en.Requests[requestID] = d.ID
	en.Used++
	s.state.Enrollments[en.ID] = en
	if e = s.save(); e != nil {
		if !statePublished(e) {
			delete(s.state.Devices, d.ID)
			s.state.Enrollments[en.ID] = previous
		}
		return Device{}, "", e
	}
	return d, updateToken, nil
}
func (s *Store) RevokeEnrollment(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.state.Enrollments[id]
	if !ok {
		return errors.New("unknown enrollment")
	}
	en := previous
	en.Revoked = true
	en.TokenCipher = ""
	s.state.Enrollments[id] = en
	if e := s.save(); e != nil {
		if !statePublished(e) {
			s.state.Enrollments[id] = previous
		}
		return e
	}
	return nil
}
func (s *Store) Enrollments(userID string) []Enrollment {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enrollmentsLocked(userID)
}
func (s *Store) enrollmentsLocked(userID string) []Enrollment {
	out := []Enrollment{}
	for _, en := range s.state.Enrollments {
		if en.UserID == userID {
			out = append(out, enrollmentMetadata(en))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Expires.Before(out[j].Expires) })
	return out
}
func (s *Store) enrollmentProfileUser(d Device) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.state.Users[d.UserID]
	current, present := s.state.Devices[d.ID]
	now := time.Now()
	if !ok || !present || u.Disabled || current.Disabled || !u.Expires.After(now) || !current.Expires.After(now) {
		return User{}, errors.New("device unavailable")
	}
	u.ID = d.ID
	u.Name = d.Name
	u.Certificate = d.Certificate
	u.Key = d.Key
	u.AWG = d.AWG
	return u, nil
}

// Domain-separated key derivation; the CA private key is already protected by
// the identity store's 0600 permissions. This is not protection from full store compromise.
func (s *Store) enrollmentCipher() (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("quic-lab/enrollment-display/v1\x00" + s.state.Key))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (s *Store) sealEnrollmentToken(id, token string) (string, error) {
	a, err := s.enrollmentCipher()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return hex.EncodeToString(a.Seal(nonce, nonce, []byte(token), []byte(id))), nil
}
func (s *Store) EnrollmentInvitation(id string) (Enrollment, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	en, ok := s.state.Enrollments[id]
	u, present := s.state.Users[en.UserID]
	if !ok || !present || u.Disabled || !u.Expires.After(time.Now()) || !enrollmentMetadata(en).CanDisplay {
		return Enrollment{}, "", errors.New("invitation unavailable")
	}
	a, err := s.enrollmentCipher()
	if err != nil {
		return Enrollment{}, "", err
	}
	raw, err := hex.DecodeString(en.TokenCipher)
	if err != nil || len(raw) < a.NonceSize() {
		return Enrollment{}, "", errors.New("invalid invitation")
	}
	token, err := a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], []byte(id))
	if err != nil {
		return Enrollment{}, "", err
	}
	if enrollmentHash(string(token)) != en.TokenHash {
		return Enrollment{}, "", errors.New("invalid invitation")
	}
	return enrollmentMetadata(en), string(token), nil
}
