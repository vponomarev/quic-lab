package vlessserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPreflightCertificateValidityAndName(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                  string
		from, to              time.Duration
		sni                   string
		missingKey, wantError bool
	}{
		{"valid", -time.Hour, time.Hour, "vpn.example", false, false},
		{"wrong-name", -time.Hour, time.Hour, "other.example", false, true},
		{"expired", -2 * time.Hour, -time.Hour, "vpn.example", false, true},
		{"future", time.Hour, 2 * time.Hour, "vpn.example", false, true},
		{"missing-key", -time.Hour, time.Hour, "vpn.example", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			c := tlsConfig()
			c.ServerName = tc.sni
			c.TLSCertificateFile = filepath.Join(dir, "cert.pem")
			c.TLSKeyFile = filepath.Join(dir, "key.pem")
			template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"vpn.example"}, NotBefore: time.Now().Add(tc.from), NotAfter: time.Now().Add(tc.to), KeyUsage: x509.KeyUsageDigitalSignature}
			der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(c.TLSCertificateFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
				t.Fatal(e)
			}
			if !tc.missingKey {
				if e = os.WriteFile(c.TLSKeyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if e = preflightCertificate(c); (e != nil) != tc.wantError {
				t.Fatalf("preflight error=%v wantError=%v", e, tc.wantError)
			}
		})
	}
}
