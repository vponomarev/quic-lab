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
	"os"
	"path/filepath"
	"quiclab/internal/gateway"
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
