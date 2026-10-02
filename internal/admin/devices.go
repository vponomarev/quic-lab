package admin

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"quiclab/internal/awgserver"
	"sort"
	"time"
)

// Device owns credentials. Legacy means all clients sharing the original user
// key are indistinguishable and are revoked together.
type Device struct {
	UpdateTokenHash     string          `json:"update_token_hash,omitempty"`
	UpdateTokenOutbox   string          `json:"update_token_outbox,omitempty"`
	UpdateOutboxExpires time.Time       `json:"update_outbox_expires,omitempty"`
	ConfigRevision      uint64          `json:"config_revision,omitempty"`
	ConfigDigest        string          `json:"config_digest,omitempty"`
	ID                  string          `json:"id"`
	UserID              string          `json:"user_id"`
	Name                string          `json:"name"`
	Certificate         string          `json:"certificate"`
	Key                 string          `json:"key,omitempty"`
	Disabled            bool            `json:"disabled,omitempty"`
	Legacy              bool            `json:"legacy,omitempty"`
	Created             time.Time       `json:"created"`
	Expires             time.Time       `json:"expires"`
	AWG                 *awgserver.Peer `json:"awg,omitempty"`
}

func legacyDevice(u User) Device {
	return Device{ID: u.ID, UserID: u.ID, Name: "Legacy device", Certificate: u.Certificate, Key: u.Key, Disabled: false, Legacy: true, Created: u.Created, Expires: u.Expires, AWG: u.AWG}
}

// Devices exposes metadata without client secrets.
func (s *Store) Devices(userID string) []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.devicesLocked(userID)
}
func (s *Store) devicesLocked(userID string) []Device {
	out := []Device{}
	for _, d := range s.state.Devices {
		if d.UserID != userID {
			continue
		}
		d.UpdateTokenHash = ""
		d.UpdateTokenOutbox = ""
		d.ConfigDigest = ""
		d.Certificate = ""
		d.Key = ""
		if d.AWG != nil {
			p := *d.AWG
			p.Private = ""
			p.PSK = ""
			d.AWG = &p
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created.Equal(out[j].Created) {
			return out[i].ID < out[j].ID
		}
		return out[i].Created.Before(out[j].Created)
	})
	return out
}
func (s *Store) deviceAllowed(cs tls.ConnectionState) (Device, error) {
	if len(cs.VerifiedChains) == 0 || len(cs.PeerCertificates) == 0 {
		return Device{}, errors.New("verified client certificate required")
	}
	cert := cs.PeerCertificates[0]
	d, ok := s.state.Devices[cert.Subject.CommonName]
	u, present := s.state.Users[d.UserID]
	now := time.Now()
	if !ok || !present || d.Disabled || u.Disabled || !d.Expires.After(now) || !u.Expires.After(now) {
		return Device{}, errors.New("client revoked or expired")
	}
	b, _ := pem.Decode([]byte(d.Certificate))
	if b == nil || sha256.Sum256(b.Bytes) != sha256.Sum256(cert.Raw) {
		return Device{}, errors.New("identity mismatch")
	}
	return d, nil
}
func (s *Store) DeviceForTLS(cs tls.ConnectionState) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, e := s.deviceAllowed(cs)
	if d.AWG != nil {
		p := *d.AWG
		d.AWG = &p
	}
	return d, e
}
func (s *Store) CreateDevice(userID, name string) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createDeviceLocked(userID, name, true)
}
func (s *Store) createDeviceLocked(userID, name string, persist bool) (Device, error) {
	u, ok := s.state.Users[userID]
	now := time.Now().UTC()
	if !ok || u.Disabled || !u.Expires.After(now) {
		return Device{}, errors.New("user unavailable")
	}
	if len(name) == 0 || len(name) > 100 {
		return Device{}, errors.New("name must contain 1..100 bytes")
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return Device{}, e
	}
	sn, e := serial()
	if e != nil {
		return Device{}, e
	}
	id, e := newDeviceID()
	if e != nil {
		return Device{}, e
	}
	d := Device{ID: id, UserID: userID, Name: name, Created: now, Expires: u.Expires}
	if d.Expires.After(s.ca.NotAfter) {
		d.Expires = s.ca.NotAfter
	}
	if !d.Expires.After(now) {
		return Device{}, errors.New("CA expired")
	}
	candidate := User{Protocols: u.Protocols}
	if e = s.provisionAWG(&candidate); e != nil {
		return Device{}, e
	}
	d.AWG = candidate.AWG
	leaf := &x509.Certificate{SerialNumber: sn, Subject: pkix.Name{CommonName: d.ID}, NotBefore: now.Add(-time.Minute), NotAfter: d.Expires, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, BasicConstraintsValid: true}
	der, e := x509.CreateCertificate(rand.Reader, leaf, s.ca, &key.PublicKey, s.key)
	if e != nil {
		return Device{}, e
	}
	kb, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return Device{}, e
	}
	d.Certificate = encode("CERTIFICATE", der) + s.state.CA
	d.Key = encode("PRIVATE KEY", kb)
	s.state.Devices[d.ID] = d
	if persist {
		if e = s.save(); e != nil {
			if !statePublished(e) {
				delete(s.state.Devices, d.ID)
			}
			return Device{}, e
		}
	}
	return d, nil
}

// The reload hook must acknowledge native peer removal before returning.
// Failures do not undo persisted revocation. All callbacks run outside mu.
func (s *Store) SetAWGReload(reload func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.awgReload = reload
}
func reloadAWG(reload func() error) error {
	if reload == nil {
		return nil
	}
	if e := reload(); e != nil {
		return fmt.Errorf("revocation persisted; AWG reload failed: %w", e)
	}
	return nil
}
func (s *Store) takeClosers(id string) []func() {
	out := []func(){}
	for token, close := range s.active[id] {
		out = append(out, close)
		delete(s.sessionProtocols, token)
	}
	delete(s.active, id)
	return out
}
func closeAll(closers []func()) {
	for _, close := range closers {
		close()
	}
}
func (s *Store) DisableDevice(id string) error {
	s.mu.Lock()
	old, ok := s.state.Devices[id]
	if !ok {
		s.mu.Unlock()
		return errors.New("unknown device")
	}
	d := old
	d.Disabled = true
	s.state.Devices[id] = d
	saveErr := s.save()
	if e := saveErr; e != nil && !statePublished(e) {
		s.state.Devices[id] = old
		s.mu.Unlock()
		return e
	}
	closers := s.takeClosers(id)
	reload := s.awgReload
	needsAWG := d.AWG != nil
	s.mu.Unlock()
	closeAll(closers)
	if needsAWG {
		return errors.Join(saveErr, reloadAWG(reload))
	}
	return saveErr
}

func newDeviceID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
