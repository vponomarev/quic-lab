package gateway

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"quiclab/internal/admin"
	"quiclab/internal/bond"
	"strings"
	"testing"
)

func TestRevocationDuringJoin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cert := &x509.Certificate{Raw: []byte("synthetic-device-certificate")}
	cs := tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
	token := strings.Repeat("ab", 32)
	ready := make(chan struct{})
	close(ready)
	entry := &bondEntry{mux: bond.NewMux(ctx, false), owner: sha256.Sum256(cert.Raw), ready: ready}
	defer entry.mux.Close()
	s := &Server{}
	s.bonds.entries = map[string]*bondEntry{token: entry}
	s.RegisterProtocol = func(tls.ConnectionState, string, func()) (func(), error) { return nil, errors.New("revoked") }
	if _, _, e := s.joinBond(ctx, ctx, cs, BondHello{Token: token, Path: "wifi"}, "quic", "127.0.0.1:1"); e == nil {
		t.Fatal("revoked device joined existing mux")
	}
}

func TestBondJoinsKeepBoundedRevocationRegistrations(t *testing.T) {
	store, e := admin.OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	u, e := store.Create("device")
	if e != nil {
		t.Fatal(e)
	}
	block, _ := pem.Decode([]byte(u.Certificate))
	cert, e := x509.ParseCertificate(block.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	cs := tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	token := strings.Repeat("ab", 32)
	ready := make(chan struct{})
	close(ready)
	entry := &bondEntry{mux: bond.NewMux(ctx, false), owner: sha256.Sum256(cert.Raw), ready: ready}
	defer entry.mux.Close()
	server := &Server{}
	server.bonds.entries = map[string]*bondEntry{token: entry}
	closed := 0
	server.RegisterProtocol = func(cs tls.ConnectionState, protocol string, close func()) (func(), error) {
		return store.RegisterProtocol(cs, protocol, func() { closed++; close() })
	}
	for i := 0; i < 100; i++ {
		if _, _, e = server.joinBond(ctx, ctx, cs, BondHello{Token: token, Path: "wifi"}, "quic", "127.0.0.1:1"); e != nil {
			t.Fatal(e)
		}
	}
	if e = store.DisableDevice(u.ID); e != nil {
		t.Fatal(e)
	}
	if closed != 1 {
		t.Fatalf("joins retained %d revocation callbacks instead of one protocol owner", closed)
	}
}
func TestRejectedLTEJoinsDoNotRetainRegistrations(t *testing.T) {
	store, e := admin.OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	u, e := store.Create("device")
	if e != nil {
		t.Fatal(e)
	}
	block, _ := pem.Decode([]byte(u.Certificate))
	cert, _ := x509.ParseCertificate(block.Bytes)
	cs := tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	token := strings.Repeat("ab", 32)
	ready := make(chan struct{})
	close(ready)
	entry := &bondEntry{mux: bond.NewMux(ctx, false), owner: sha256.Sum256(cert.Raw), ready: ready}
	defer entry.mux.Close()
	entry.mux.Session.BlockCell()
	server := &Server{}
	server.bonds.entries = map[string]*bondEntry{token: entry}
	closed := 0
	server.RegisterProtocol = func(cs tls.ConnectionState, protocol string, close func()) (func(), error) {
		return store.RegisterProtocol(cs, protocol, func() { closed++; close() })
	}
	for i := 0; i < 50; i++ {
		if _, _, e = server.joinBond(ctx, ctx, cs, BondHello{Token: token, Path: "cell"}, "quic", "127.0.0.1:1"); e == nil {
			t.Fatal("blocked LTE joined")
		}
	}
	store.DisableDevice(u.ID)
	if closed != 0 {
		t.Fatalf("rejected LTE joins retained %d callbacks", closed)
	}
}
