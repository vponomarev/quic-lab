// Package admin provisions client identities and manages revocation for the gateway.
package admin

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"quiclab/internal/awgserver"
	"quiclab/internal/vlessserver"
	"sort"
	"sync"
	"time"
)

type User struct {
	Enrollments   []Enrollment    `json:"-"`
	Devices       []Device        `json:"-"`
	Disabled      bool            `json:"disabled,omitempty"`
	Protocols     []string        `json:"protocols"`
	AWG           *awgserver.Peer `json:"awg,omitempty"`
	LastConnected time.Time       `json:"last_connected,omitempty"`
	LastTransport string          `json:"last_transport,omitempty"`
	LastSource    string          `json:"last_source,omitempty"`
	Stats         UserStats       `json:"-"`
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Created       time.Time       `json:"created"`
	Expires       time.Time       `json:"expires"`
	Certificate   string          `json:"certificate"`
	Key           string          `json:"key,omitempty"`
}
type diskState struct {
	VLESS         *vlessserver.Config   `json:"vless,omitempty"`
	VLESSRevision uint64                `json:"vless_revision,omitempty"`
	VLESSDigest   string                `json:"vless_digest,omitempty"`
	Enrollments   map[string]Enrollment `json:"enrollments,omitempty"`
	AWG           *awgserver.Identity   `json:"awg,omitempty"`
	Version       int                   `json:"version"`
	CA            string                `json:"ca"`
	Key           string                `json:"ca_key"`
	Users         map[string]User       `json:"users"`
	Devices       map[string]Device     `json:"devices"`
}
type Store struct {
	vlessSyncMu       sync.Mutex
	vlessClient       *vlessserver.ControlClient
	vlessApplied      *vlessserver.Snapshot
	vlessError        bool
	vlessWritePending bool
	capture           *captureState
	admission         *Admission
	awgConfig         *awgserver.Config
	awgPrevious       map[string]awgserver.PeerStatus
	awgUpdated        time.Time
	awgStarted        time.Time
	sessionProtocols  map[string]string
	stats             map[string]*userTraffic
	mu                sync.Mutex
	path              string
	state             diskState
	ca                *x509.Certificate
	key               *ecdsa.PrivateKey
	active            map[string]map[string]func()
	awgReload         func() error
	syncDir           func(string) error
}

func randomID() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func serial() (*big.Int, error) { return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128)) }
func encode(kind string, b []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: b}))
}
func OpenStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("data_dir required")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	s := &Store{admission: NewAdmission(DefaultDeviceLimit), path: filepath.Join(dir, "identities.json"), active: make(map[string]map[string]func())}
	b, e := os.ReadFile(s.path)
	if e == nil {
		if e = json.Unmarshal(b, &s.state); e != nil {
			return nil, e
		}
		if (s.state.Version != 1 && s.state.Version != 2) || s.state.Users == nil {
			return nil, errors.New("invalid identity store")
		}
		pair, e := tls.X509KeyPair([]byte(s.state.CA), []byte(s.state.Key))
		if e != nil {
			return nil, e
		}
		s.ca, e = x509.ParseCertificate(pair.Certificate[0])
		if e != nil {
			return nil, e
		}
		var ok bool
		s.key, ok = pair.PrivateKey.(*ecdsa.PrivateKey)
		if !ok || !s.ca.IsCA {
			return nil, errors.New("invalid CA")
		}
		if s.state.Version == 1 {
			s.state.Devices = make(map[string]Device)
			for _, u := range s.state.Users {
				legacy := legacyDevice(u)

				s.state.Devices[u.ID] = legacy
			}
			s.state.Version = 2
			if e = s.save(); e != nil {
				return nil, e
			}
		} else if s.state.Devices == nil {
			return nil, errors.New("invalid device store")
		}
		return s, nil
	}
	if !os.IsNotExist(e) {
		return nil, e
	}
	k, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return nil, e
	}
	sn, e := serial()
	if e != nil {
		return nil, e
	}
	c := &x509.Certificate{SerialNumber: sn, Subject: pkix.Name{CommonName: "QUIC Lab managed client CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(5, 0, 0)}
	der, e := x509.CreateCertificate(rand.Reader, c, c, &k.PublicKey, k)
	if e != nil {
		return nil, e
	}
	s.ca, e = x509.ParseCertificate(der)
	if e != nil {
		return nil, e
	}
	s.key = k
	kd, e := x509.MarshalPKCS8PrivateKey(k)
	if e != nil {
		return nil, e
	}
	s.state = diskState{Version: 2, Devices: make(map[string]Device), CA: encode("CERTIFICATE", der), Key: encode("PRIVATE KEY", kd), Users: make(map[string]User)}
	if e = s.save(); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *Store) save() error {
	b, e := json.MarshalIndent(s.state, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(s.path), ".identities-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if old, err := os.ReadFile(s.path); err == nil {
		backup, err := os.CreateTemp(filepath.Dir(s.path), ".identities-backup-*")
		if err != nil {
			return err
		}
		backupName := backup.Name()
		defer os.Remove(backupName)
		if err = backup.Chmod(0600); err == nil {
			_, err = backup.Write(old)
		}
		if err == nil {
			err = backup.Sync()
		}
		closeErr := backup.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err = os.Rename(backupName, s.path+".bak"); err != nil {
			return err
		}
		if err = s.syncStateDirectory(); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if e = os.Rename(name, s.path); e != nil {
		return e
	}
	if e = s.syncStateDirectory(); e != nil {
		return &publishedSaveError{e}
	}
	return nil
}
func (s *Store) Create(name string) (User, error) { return s.CreateWithProtocols(name, nil) }
func (s *Store) CreateWithProtocols(name string, protocols []string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(name) == 0 || len(name) > 100 {
		return User{}, errors.New("name must contain 1..100 bytes")
	}
	if len(s.state.Users) >= 1000 {
		return User{}, errors.New("user limit")
	}
	for _, u := range s.state.Users {
		if u.Name == name {
			return User{}, errors.New("name already exists")
		}
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return User{}, e
	}
	sn, e := serial()
	if e != nil {
		return User{}, e
	}
	now := time.Now().UTC()
	if protocols != nil {
		protocols = append([]string{}, protocols...)
	}
	u := User{ID: randomID(), Name: name, Created: now, Expires: now.AddDate(0, 0, 90), Protocols: protocols}
	if e := s.provisionAWG(&u); e != nil {
		return User{}, e
	}
	if u.Expires.After(s.ca.NotAfter) {
		u.Expires = s.ca.NotAfter
	}
	if !u.Expires.After(now) {
		return User{}, errors.New("CA expired")
	}
	leaf := &x509.Certificate{SerialNumber: sn, Subject: pkix.Name{CommonName: u.ID}, NotBefore: now.Add(-time.Minute), NotAfter: u.Expires, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, BasicConstraintsValid: true}
	der, e := x509.CreateCertificate(rand.Reader, leaf, s.ca, &key.PublicKey, s.key)
	if e != nil {
		return User{}, e
	}
	kb, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return User{}, e
	}
	u.Certificate = encode("CERTIFICATE", der) + s.state.CA
	u.Key = encode("PRIVATE KEY", kb)
	s.state.Users[u.ID] = u
	legacy := legacyDevice(u)
	if e := s.provisionVLESS(&legacy, u); e != nil {
		delete(s.state.Users, u.ID)
		return User{}, e
	}
	s.state.Devices[u.ID] = legacy
	if e = s.save(); e != nil {
		if !statePublished(e) {
			delete(s.state.Users, u.ID)
			delete(s.state.Devices, u.ID)
		}
		return User{}, e
	}
	return u, nil
}

// Rename changes only the display name; credentials and live tunnels remain valid.
func (s *Store) Rename(id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.state.Users[id]
	if !ok {
		return errors.New("unknown user")
	}
	if len(name) == 0 || len(name) > 100 {
		return errors.New("name must contain 1..100 bytes")
	}
	for otherID, user := range s.state.Users {
		if otherID != id && user.Name == name {
			return errors.New("name already exists")
		}
	}
	updated := old
	updated.Name = name
	s.state.Users[id] = updated
	if e := s.save(); e != nil {
		if !statePublished(e) {
			s.state.Users[id] = old
		}
		return e
	}
	return nil
}

func (s *Store) List() []User {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]User, 0, len(s.state.Users))
	for _, u := range s.state.Users {
		u.Stats = s.snapshot(u.ID, time.Now())
		u.Devices = s.devicesLocked(u.ID)
		u.Enrollments = s.enrollmentsLocked(u.ID)
		u.Key = ""
		u.Certificate = ""
		if u.AWG != nil {
			peer := *u.AWG
			peer.Private = ""
			peer.PSK = ""
			u.AWG = &peer
		}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}
func (s *Store) Profile(id string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.state.Users[id]
	if !ok || u.Disabled || s.state.Devices[id].Disabled || time.Now().After(u.Expires) {
		return User{}, errors.New("user unavailable")
	}
	return u, nil
}
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	u, ok := s.state.Users[id]
	if !ok {
		s.mu.Unlock()
		return errors.New("unknown user")
	}
	removed := map[string]Device{}
	needsAWG := false
	for deviceID, d := range s.state.Devices {
		if d.UserID == id {
			removed[deviceID] = d
			delete(s.state.Devices, deviceID)
			needsAWG = needsAWG || d.AWG != nil
		}
	}
	delete(s.state.Users, id)
	saveErr := s.save()
	if e := saveErr; e != nil && !statePublished(e) {
		s.state.Users[id] = u
		for k, d := range removed {
			s.state.Devices[k] = d
		}
		s.mu.Unlock()
		return e
	}
	delete(s.stats, id)
	closers := []func(){}
	for deviceID := range removed {
		closers = append(closers, s.takeClosers(deviceID)...)
	}
	reload := s.awgReload
	s.mu.Unlock()
	closeAll(closers)
	saveErr = errors.Join(saveErr, s.SyncVLESS(context.Background()))
	if needsAWG {
		return errors.Join(saveErr, reloadAWG(reload))
	}
	return saveErr
}
func (s *Store) allowed(cs tls.ConnectionState) (string, error) {
	d, e := s.deviceAllowed(cs)
	return d.UserID, e
}
func (s *Store) Verify(cs tls.ConnectionState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, e := s.allowed(cs)
	return e
}

// Register closes live sessions on deletion and serializes the register/delete race.
func (s *Store) Register(cs tls.ConnectionState, close func()) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, e := s.deviceAllowed(cs)
	if e != nil {
		return nil, e
	}
	releaseAdmission, e := s.admission.Acquire(d.ID)
	if e != nil {
		return nil, e
	}
	id := d.ID
	token := randomID()
	if s.active[id] == nil {
		s.active[id] = make(map[string]func())
	}
	s.active[id][token] = func() { releaseAdmission(); close() }
	return func() {
		releaseAdmission()
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.active[id], token)
		if len(s.active[id]) == 0 {
			delete(s.active, id)
		}
	}, nil
}
func (s *Store) TLS(cert tls.Certificate) *tls.Config {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(s.state.CA))
	return &tls.Config{Certificates: []tls.Certificate{cert}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, VerifyConnection: s.Verify}
}
func (s *Store) String() string {
	return fmt.Sprintf("managed identity store (%d users)", len(s.List()))
}

// A directory sync failure after primary rename cannot be rolled back by
// restoring memory: readers already see the published primary file.
type publishedSaveError struct{ cause error }

func (e *publishedSaveError) Error() string {
	return "identity state published; directory sync failed: " + e.cause.Error()
}
func (e *publishedSaveError) Unwrap() error { return e.cause }
func statePublished(err error) bool {
	var published *publishedSaveError
	return errors.As(err, &published)
}
func (s *Store) syncStateDirectory() error {
	dir := filepath.Dir(s.path)
	if s.syncDir != nil {
		return s.syncDir(dir)
	}
	f, e := os.Open(dir)
	if e != nil {
		return e
	}
	e = f.Sync()
	return errors.Join(e, f.Close())
}
