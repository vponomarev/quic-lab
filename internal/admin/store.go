// Package admin provisions client identities and manages revocation for the gateway.
package admin

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
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
	"sort"
	"sync"
	"time"
)

type User struct {
	LastConnected time.Time `json:"last_connected,omitempty"`
	LastTransport string    `json:"last_transport,omitempty"`
	LastSource    string    `json:"last_source,omitempty"`
	Stats         UserStats `json:"-"`
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Created       time.Time `json:"created"`
	Expires       time.Time `json:"expires"`
	Certificate   string    `json:"certificate"`
	Key           string    `json:"key,omitempty"`
}
type diskState struct {
	Version int             `json:"version"`
	CA      string          `json:"ca"`
	Key     string          `json:"ca_key"`
	Users   map[string]User `json:"users"`
}
type Store struct {
	stats  map[string]*userTraffic
	mu     sync.Mutex
	path   string
	state  diskState
	ca     *x509.Certificate
	key    *ecdsa.PrivateKey
	active map[string]map[string]func()
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
	s := &Store{path: filepath.Join(dir, "identities.json"), active: make(map[string]map[string]func())}
	b, e := os.ReadFile(s.path)
	if e == nil {
		if e = json.Unmarshal(b, &s.state); e != nil {
			return nil, e
		}
		if s.state.Version != 1 || s.state.Users == nil {
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
	s.state = diskState{1, encode("CERTIFICATE", der), encode("PRIVATE KEY", kd), make(map[string]User)}
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
	return os.Rename(name, s.path)
}
func (s *Store) Create(name string) (User, error) {
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
	u := User{ID: randomID(), Name: name, Created: now, Expires: now.AddDate(0, 0, 90)}
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
	if e = s.save(); e != nil {
		delete(s.state.Users, u.ID)
		return User{}, e
	}
	return u, nil
}
func (s *Store) List() []User {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]User, 0, len(s.state.Users))
	for _, u := range s.state.Users {
		u.Stats = s.snapshot(u.ID, time.Now())
		u.Key = ""
		u.Certificate = ""
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}
func (s *Store) Profile(id string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.state.Users[id]
	if !ok || time.Now().After(u.Expires) {
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
	delete(s.state.Users, id)
	if e := s.save(); e != nil {
		s.state.Users[id] = u
		s.mu.Unlock()
		return e
	}
	delete(s.stats, id)
	closers := s.active[id]
	delete(s.active, id)
	s.mu.Unlock()
	for _, close := range closers {
		close()
	}
	return nil
}
func (s *Store) allowed(cs tls.ConnectionState) (string, error) {
	if len(cs.VerifiedChains) == 0 || len(cs.PeerCertificates) == 0 {
		return "", errors.New("verified client certificate required")
	}
	cert := cs.PeerCertificates[0]
	u, ok := s.state.Users[cert.Subject.CommonName]
	if !ok || time.Now().After(u.Expires) {
		return "", errors.New("client revoked or expired")
	}
	b, _ := pem.Decode([]byte(u.Certificate))
	if b == nil || sha256.Sum256(b.Bytes) != sha256.Sum256(cert.Raw) {
		return "", errors.New("identity mismatch")
	}
	return u.ID, nil
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
	id, e := s.allowed(cs)
	if e != nil {
		return nil, e
	}
	token := randomID()
	if s.active[id] == nil {
		s.active[id] = make(map[string]func())
	}
	s.active[id][token] = close
	return func() {
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
