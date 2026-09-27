package labcert

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Reloadable keeps the last good pair. Reload never modifies existing TLS connections.
type Reloadable struct {
	cert atomic.Pointer[tls.Certificate]
}

func NewReloadable(c tls.Certificate) *Reloadable { r := new(Reloadable); r.cert.Store(&c); return r }
func (r *Reloadable) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return r.cert.Load(), nil
}
func (r *Reloadable) Reload(certPath, keyPath string) error {
	// Resolve a shared "current" symlink once so an atomic rotation cannot mix generations.
	if filepath.Dir(certPath) == filepath.Dir(keyPath) {
		dir, err := filepath.EvalSymlinks(filepath.Dir(certPath))
		if err != nil {
			return err
		}
		certPath = filepath.Join(dir, filepath.Base(certPath))
		keyPath = filepath.Join(dir, filepath.Base(keyPath))
	}
	cp, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	kp, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	c, err := tls.X509KeyPair(cp, kp)
	if err != nil {
		return err
	}
	leaf, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		return err
	}
	if time.Now().Before(leaf.NotBefore) || time.Now().After(leaf.NotAfter) {
		return fmt.Errorf("replacement certificate is not currently valid")
	}
	old := r.cert.Load()
	if old != nil && len(old.Certificate) > 0 {
		prior, e := x509.ParseCertificate(old.Certificate[0])
		if e != nil {
			return e
		}
		for _, name := range prior.DNSNames {
			if e = leaf.VerifyHostname(name); e != nil {
				return fmt.Errorf("replacement certificate loses hostname %s", name)
			}
		}
	}
	c.Leaf = leaf
	r.cert.Store(&c)
	return nil
}
