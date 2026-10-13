//go:build linux

package vpnclienttest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"github.com/quic-go/quic-go"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"quiclab/internal/gateway"
	"syscall"
	"testing"
	"time"
)

func TestNamespaceFixture(t *testing.T) {
	dir := os.Getenv("VPN_PROBE_FIXTURE_DIR")
	if dir == "" {
		t.Skip("isolated live fixture only")
	}
	here, _ := os.Readlink("/proc/self/ns/net")
	host, _ := os.Readlink("/proc/1/ns/net")
	if here == host {
		t.Fatal("fixture requires netns")
	}
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cert := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &k.PublicKey, k)
	if e != nil {
		t.Fatal(e)
	}
	kb, _ := x509.MarshalPKCS8PrivateKey(k)
	cp := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kp := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb})
	pair, _ := tls.X509KeyPair(cp, kp)
	ca := filepath.Join(dir, "ca.pem")
	os.WriteFile(ca, cp, 0600)
	tc, e := gateway.TLS(pair, ca)
	if e != nil {
		t.Fatal(e)
	}
	tcp, e := net.Listen("tcp4", "192.0.2.2:9001")
	if e != nil {
		t.Fatal(e)
	}
	defer tcp.Close()
	go func() {
		for {
			c, e := tcp.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	udp, e := net.ListenPacket("udp4", "192.0.2.2:9002")
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	go func() {
		b := make([]byte, 4096)
		for {
			n, a, e := udp.ReadFrom(b)
			if e != nil {
				return
			}
			udp.WriteTo(b[:n], a)
		}
	}()
	srv, e := gateway.New("192.0.2.2/32", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer cancel()
	mode := os.Getenv("VPN_PROBE_TRANSPORT")
	if mode == "https" {
		tc.NextProtos = []string{"http/1.1"}
		ln, e := tls.Listen("tcp", "198.18.0.2:9443", tc)
		if e != nil {
			t.Fatal(e)
		}
		hs := &http.Server{Handler: http.HandlerFunc(srv.WebSocket)}
		defer hs.Close()
		go hs.Serve(ln)
	} else {
		ln, e := quic.ListenAddr("198.18.0.2:9443", tc, &quic.Config{EnableDatagrams: true, MaxIncomingStreams: 128})
		if e != nil {
			t.Fatal(e)
		}
		defer ln.Close()
		go srv.ServeQUIC(ctx, ln)
	}
	raw, _ := json.Marshal(map[string]string{"transport": mode, "endpoint": "198.18.0.2:9443", "hostname": "localhost", "certificate": string(cp), "key": string(kp), "ca": string(cp)})
	if e = os.WriteFile(filepath.Join(dir, "client.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	select {
	case <-ctx.Done():
	case <-time.After(90 * time.Second):
		t.Fatal("fixture timeout")
	}
}
