package labcert

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

func TestCertificateReloadRetainsLastGood(t *testing.T) {
	cp, kp, _ := Generate()
	c, _ := tls.X509KeyPair(cp, kp)
	r := NewReloadable(c)
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	os.WriteFile(cert, cp, 0600)
	os.WriteFile(key, kp, 0600)
	if e := r.Reload(cert, key); e != nil {
		t.Fatal(e)
	}
	old, _ := r.GetCertificate(nil)
	os.WriteFile(key, []byte("bad key"), 0600)
	if r.Reload(cert, key) == nil {
		t.Fatal("invalid pair accepted")
	}
	current, _ := r.GetCertificate(nil)
	if current != old {
		t.Fatal("last good pair replaced")
	}
}
