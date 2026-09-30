package mobile

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"github.com/quic-go/quic-go"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"quiclab/internal/bond"
	"quiclab/internal/gateway"
	"sync/atomic"
	"testing"
	"time"
)

func TestBondTCPPreservesExitConnection(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(ca, []byte(cp), 0600)
	tc, _ := gateway.TLS(pair, ca)
	srv, _ := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, e := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{EnableDatagrams: true})
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	go srv.ServeQUIC(ctx, ln)
	target, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	accepted := make(chan net.Conn, 2)
	go func() {
		for {
			c, e := target.Accept()
			if e != nil {
				return
			}
			accepted <- c
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	g := NewGateway(nil)
	defer g.Stop()
	cfg, _ := json.Marshal(gatewayConfig{MaxAvailability: true, Transport: "quic", Endpoint: ln.Addr().String(), Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
	if e = g.Start(string(cfg), nil); e != nil {
		t.Fatal(e)
	}
	if e = g.EnsureBondPath("cell", nil); e != nil {
		t.Fatal(e)
	}

	// Even possession of the join token cannot cross the certificate boundary.
	issuer, _ := x509.ParseCertificate(pair.Certificate[0])
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := *issuer
	leaf.SerialNumber = big.NewInt(42)
	leaf.IsCA = false
	leaf.KeyUsage = x509.KeyUsageDigitalSignature
	der, e := x509.CreateCertificate(rand.Reader, &leaf, issuer, &key.PublicKey, pair.PrivateKey)
	if e != nil {
		t.Fatal(e)
	}
	rawKey, _ := x509.MarshalPKCS8PrivateKey(key)
	otherCert, _ := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: rawKey}))
	intruder := NewGateway(nil)
	intruder.cfg = g.cfg
	intruder.tls = g.tls.Clone()
	intruder.tls.Certificates = []tls.Certificate{otherCert}
	intruder.ctx, intruder.cancel = context.WithCancel(context.Background())
	defer intruder.cancel()
	intruder.bondToken = g.bondToken
	if e = intruder.addBondPath("cell", nil, false); e == nil {
		t.Fatal("another certificate joined victim session")
	}
	st, e := g.dialStream(ctx, "tcp", target.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	st.SetDeadline(time.Now().Add(8 * time.Second))
	for _, path := range []string{"wifi", "cell"} {
		g.DropBondPath(path)
		payload := bytes.Repeat([]byte(path), 1000)
		if _, e = st.Write(payload); e != nil {
			t.Fatal(e)
		}
		b := make([]byte, len(payload))
		if _, e = io.ReadFull(st, b); e != nil || !bytes.Equal(b, payload) {
			t.Fatal("stream changed on path loss", e)
		}
		if e = g.EnsureBondPath(path, nil); e != nil {
			t.Fatal(e)
		}
	}
	select {
	case c := <-accepted:
		defer c.Close()
	default:
		t.Fatal("no exit connection")
	}
	select {
	case c := <-accepted:
		c.Close()
		t.Fatal("exit connection recreated")
	default:
	}
	// New flows rotate to a fresh session; the already-open socket stays alive.
	oldMux, oldToken := g.bond, g.bondToken
	oldMux.BeginDrain()
	// Until the shared LTE ledger is wired (C1), rotation must not reset a finite budget.
	g.cfg.BondCellBudget = 1024
	if _, err := g.dialStream(ctx, "tcp", target.Addr().String()); err == nil {
		t.Fatal("rotation reset finite LTE budget")
	}
	if g.bond != oldMux || g.bondToken != oldToken {
		t.Fatal("failed rotation changed session")
	}
	g.cfg.BondCellBudget = 0
	newer, err := g.dialStream(ctx, "tcp", target.Addr().String())
	if err != nil {
		t.Fatal("draining session failed to rotate", err)
	}
	defer newer.Close()
	if g.bond == oldMux || g.bondToken == oldToken {
		t.Fatal("session identity reused")
	}
	if _, err = st.Write([]byte("old")); err != nil {
		t.Fatal(err)
	}
	retained := make([]byte, 3)
	if _, err = io.ReadFull(st, retained); err != nil || string(retained) != "old" {
		t.Fatal("drain killed old stream", err)
	}
	st.CloseWrite()
	b := make([]byte, 1)
	if _, e = st.Read(b); e != io.EOF {
		t.Fatalf("half-close: %v", e)
	}
	g.bond.Session.BlockCell()
	if e = g.RestartBond("wifi", nil); e != nil {
		t.Fatal(e)
	}
	if g.BondCellAllowed() {
		t.Fatal("logical-session recovery reset blocked LTE budget")
	}
	if e = g.EnsureBondPath("cell", nil); e == nil {
		t.Fatal("blocked LTE reconnected")
	}

}

func TestBondCreateNeverReusesCallerToken(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(ca, []byte(cp), 0600)
	tc, _ := gateway.TLS(pair, ca)
	srv, _ := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.BondOptions = bond.Options{DisconnectGrace: 200 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, e := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{EnableDatagrams: true})
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	go srv.ServeQUIC(ctx, ln)
	g := NewGateway(nil)
	defer g.Stop()
	cfg, _ := json.Marshal(gatewayConfig{MaxAvailability: true, Transport: "quic", Endpoint: ln.Addr().String(), Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
	if e = g.Start(string(cfg), nil); e != nil {
		t.Fatal(e)
	}
	old := g.bondToken
	// Replaying CREATE must not attach to an existing session with the caller's token.
	if e = g.addBondPath("wifi", nil, true); e != nil {
		t.Fatal(e)
	}
	if g.bondToken == old {
		t.Fatal("CREATE reused caller token")
	}
	time.Sleep(400 * time.Millisecond)
	fresh := g.bondToken
	g.bondToken = old
	if e = g.addBondPath("cell", nil, false); e == nil {
		t.Fatal("expired token resumed old session")
	}
	g.bondToken = fresh
}

func TestQUICHTTPSShareOneExitSocket(t *testing.T) { testBondTransportContinuity(t, false) }
func TestHTTPSQUICShareOneExitSocket(t *testing.T) { testBondTransportContinuity(t, true) }
func testBondTransportContinuity(t *testing.T, startHTTPS bool) {
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(ca, []byte(cp), 0600)
	tc, _ := gateway.TLS(pair, ca)
	srv, _ := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.BondContext = ctx
	q, e := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{EnableDatagrams: true})
	if e != nil {
		t.Fatal(e)
	}
	defer q.Close()
	go srv.ServeQUIC(ctx, q)
	ht := tc.Clone()
	ht.NextProtos = []string{"http/1.1"}
	ln, e := tls.Listen("tcp", "127.0.0.1:0", ht)
	if e != nil {
		t.Fatal(e)
	}
	hs := &http.Server{Handler: http.HandlerFunc(srv.ServeBondHTTPS)}
	defer hs.Close()
	go hs.Serve(ln)
	target, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	var count atomic.Int32
	go func() {
		for {
			c, e := target.Accept()
			if e != nil {
				return
			}
			count.Add(1)
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	g := NewGateway(nil)
	defer g.Stop()
	transport, endpoint := "quic", q.Addr().String()
	if startHTTPS {
		transport, endpoint = "https", ln.Addr().String()
	}
	cfg, _ := json.Marshal(gatewayConfig{MaxAvailability: true, Transport: transport, Endpoint: endpoint, Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
	if e = g.Start(string(cfg), nil); e != nil {
		t.Fatal(e)
	}
	noCert := NewGateway(nil)
	noCert.cfg = g.cfg
	noCert.tls = g.tls.Clone()
	noCert.tls.Certificates = nil
	noCert.bondToken = g.bondToken
	if err := noCert.addBondHTTPS(ctx, ln.Addr().String(), bond.PathInfo{ID: "unauthorized", ProfileID: "fallback", Network: "wifi", Generation: 1}, nil, false); err == nil {
		t.Fatal("client without certificate joined")
	}
	st, e := g.dialStream(ctx, "tcp", target.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	st.SetDeadline(time.Now().Add(4 * time.Second))
	if startHTTPS {
		g.cfg.Transport = "quic"
		g.cfg.Endpoint = q.Addr().String()
		if e = g.addBondPath("cell", nil, false); e != nil {
			t.Fatal(e)
		}
		g.cfg.Transport = transport
		g.cfg.Endpoint = endpoint
	} else {
		info := bond.PathInfo{ID: "fallback-https", ProfileID: "fallback", Network: "wifi", Generation: 1}
		if e = g.addBondHTTPS(ctx, ln.Addr().String(), info, nil, false); e != nil {
			t.Fatal(e)
		}
	}
	udp, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	seen := make(chan string, 2)
	go func() {
		buf := make([]byte, 256)
		for {
			n, from, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			seen <- from.String()
			udp.WriteTo(buf[:n], from)
		}
	}()
	mapping, e := (bondUDP{g}).DialContext(ctx, "udp4", udp.LocalAddr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer mapping.Close()
	mapping.SetDeadline(time.Now().Add(3 * time.Second))
	if _, e = mapping.Write([]byte("before")); e != nil {
		t.Fatal(e)
	}
	udpBuf := make([]byte, 16)
	if _, e = mapping.Read(udpBuf); e != nil {
		t.Fatal(e)
	}
	before := <-seen
	g.DropBondPath("wifi")
	if _, e = mapping.Write([]byte("after")); e != nil {
		t.Fatal(e)
	}
	if _, e = mapping.Read(udpBuf); e != nil {
		t.Fatal(e)
	}
	if after := <-seen; after != before {
		t.Fatal("UDP mapping changed", before, after)
	}
	payload := []byte("same exit socket")
	if _, e = st.Write(payload); e != nil {
		t.Fatal(e)
	}
	got := make([]byte, len(payload))
	if _, e = io.ReadFull(st, got); e != nil || !bytes.Equal(got, payload) {
		t.Fatal("HTTPS fallback", e)
	}
	if count.Load() != 1 {
		t.Fatal("exit socket recreated", count.Load())
	}
}
