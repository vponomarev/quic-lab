package servertls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"github.com/quic-go/quic-go"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func identity(t *testing.T, expired bool) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-2 * time.Hour), NotAfter: now.Add(time.Hour)}
	der, e := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	root, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"vpn.test"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if expired {
		leaf.NotAfter = now.Add(-time.Minute)
	}
	cert, e := x509.CreateCertificate(rand.Reader, leaf, root, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	return tls.Certificate{Certificate: [][]byte{cert, der}, PrivateKey: key}, roots
}
func TestDifferentSNIStillVerifiesServer(t *testing.T) {
	pair, roots := identity(t, false)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS.ServerName != "cover.test" {
			t.Errorf("sent SNI %q", r.TLS.ServerName)
		}
		io.WriteString(w, "ok")
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()
	cfg, e := ClientConfig(ClientOptions{ServerName: "cover.test", VerifyName: "vpn.test", Roots: roots})
	if e != nil {
		t.Fatal(e)
	}
	cfg.ClientSessionCache = tls.NewLRUClientSessionCache(2)
	for i := 0; i < 2; i++ {
		tr := &http.Transport{TLSClientConfig: cfg, DisableKeepAlives: true}
		client := &http.Client{Transport: tr}
		resp, e := client.Get(srv.URL)
		if e != nil {
			t.Fatal(e)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		tr.CloseIdleConnections()
		if i == 1 && !resp.TLS.DidResume {
			t.Fatal("resumption not exercised")
		}
	}
}
func TestRejectWrongExpiredCertificate(t *testing.T) {
	for _, kind := range []string{"name", "ca", "expired"} {
		t.Run(kind, func(t *testing.T) {
			pair, roots := identity(t, kind == "expired")
			name := "vpn.test"
			if kind == "name" {
				name = "wrong.test"
			}
			if kind == "ca" {
				_, roots = identity(t, false)
			}
			cfg, e := ClientConfig(ClientOptions{ServerName: "cover.test", VerifyName: name, Roots: roots})
			if e != nil {
				t.Fatal(e)
			}
			leaf, _ := x509.ParseCertificate(pair.Certificate[0])
			root, _ := x509.ParseCertificate(pair.Certificate[1])
			for _, resumed := range []bool{false, true} {
				if cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf, root}, DidResume: resumed}) == nil {
					t.Fatalf("accepted %s, resumed=%v", kind, resumed)
				}
			}
		})
	}
	if _, e := ClientConfig(ClientOptions{ServerName: "cover.test"}); e == nil {
		t.Fatal("missing verified name accepted")
	}
}

func TestQUICAndHTTPSCustomSNI(t *testing.T) {
	pair, roots := identity(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	listener, err := quic.ListenAddr("127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13, NextProtos: []string{"test-vpn"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	seen := make(chan string, 1)
	go func() {
		c, err := listener.Accept(ctx)
		if err == nil {
			seen <- c.ConnectionState().TLS.ServerName
			<-ctx.Done()
			c.CloseWithError(0, "")
		}
	}()
	cfg, err := ClientConfig(ClientOptions{ServerName: "cover.test", VerifyName: "vpn.test", Roots: roots})
	if err != nil {
		t.Fatal(err)
	}
	cfg.NextProtos = []string{"test-vpn"}
	c, err := quic.DialAddr(ctx, listener.Addr().String(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseWithError(0, "")
	select {
	case name := <-seen:
		if name != "cover.test" {
			t.Fatalf("sent SNI %q", name)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
