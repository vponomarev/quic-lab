package mobile

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"quiclab/internal/gateway"
)

func testIdentity(t *testing.T) (tls.Certificate, string, string) {
	t.Helper()
	k, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &k.PublicKey, k)
	if e != nil {
		t.Fatal(e)
	}
	key, _ := x509.MarshalPKCS8PrivateKey(k)
	cp := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	kp := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}))
	pair, e := tls.X509KeyPair([]byte(cp), []byte(kp))
	if e != nil {
		t.Fatal(e)
	}
	return pair, cp, kp
}
func TestGatewayTransports(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(ca, []byte(cp), 0600)
	tc, e := gateway.TLS(pair, ca)
	if e != nil {
		t.Fatal(e)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, _ := gateway.New("127.0.0.0/8", log)
	for _, mode := range []string{"quic", "https"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var addr string
			if mode == "quic" {
				ln, e := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{MaxIncomingStreams: 128})
				if e != nil {
					t.Fatal(e)
				}
				defer ln.Close()
				addr = ln.Addr().String()
				go srv.ServeQUIC(ctx, ln)
			} else {
				cfg := tc.Clone()
				cfg.NextProtos = []string{"http/1.1"}
				ln, e := tls.Listen("tcp", "127.0.0.1:0", cfg)
				if e != nil {
					t.Fatal(e)
				}
				hs := &http.Server{Handler: http.HandlerFunc(srv.WebSocket)}
				defer hs.Close()
				addr = ln.Addr().String()
				go hs.Serve(ln)
			}
			g := NewGateway(nil)
			cfg, _ := json.Marshal(gatewayConfig{Transport: mode, Endpoint: addr, Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
			if e := g.Start(string(cfg), nil); e != nil {
				t.Fatal(e)
			}
			defer g.Stop()
			target, e := net.Listen("tcp4", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer target.Close()
			body := bytes.Repeat([]byte("download-data"), 100000)
			done := make(chan error, 1)
			go func() {
				c, e := target.Accept()
				if e != nil {
					done <- e
					return
				}
				defer c.Close()
				b, e := io.ReadAll(c)
				if e != nil {
					done <- e
					return
				}
				if string(b) != "request" {
					done <- io.ErrUnexpectedEOF
					return
				}
				_, e = c.Write(body)
				done <- e
			}()
			s, e := gateway.Open(ctx, g.open, "tcp", target.Addr().String())
			if e != nil {
				t.Fatal(e)
			}
			s.SetDeadline(time.Now().Add(10 * time.Second))
			if _, e = s.Write([]byte("request")); e != nil {
				t.Fatal(e)
			}
			s.CloseWrite()
			if mode == "quic" {
				if e := g.PreparePath("next", nil); e != nil {
					t.Fatal(e)
				}
				if e := g.MigrateTo("next", nil); e != nil {
					t.Fatal(e)
				}
			}
			got, e := io.ReadAll(s)
			s.Close()
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(got, body) {
				t.Fatalf("truncated response: %d / %d", len(got), len(body))
			}
			if e := <-done; e != nil {
				t.Fatal(e)
			}
			if _, e = gateway.Open(ctx, g.open, "tcp", "10.1.2.3:80"); e == nil {
				t.Fatal("ACL bypass")
			}
			bad := NewGateway(nil)
			_, other, otherKey := testIdentity(t)
			bcfg, _ := json.Marshal(gatewayConfig{Transport: mode, Endpoint: addr, Hostname: "localhost", Certificate: other, Key: otherKey, CA: cp})
			if e := bad.Start(string(bcfg), nil); e == nil {
				bad.Stop()
				t.Fatal("untrusted identity accepted")
			}
			// Closing the tunnel must unblock an established proxied read.
			idle, e := net.Listen("tcp4", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer idle.Close()
			accepted := make(chan net.Conn, 1)
			go func() {
				c, e := idle.Accept()
				if e == nil {
					accepted <- c
				}
			}()
			st, e := gateway.Open(ctx, g.open, "tcp", idle.Addr().String())
			if e != nil {
				t.Fatal(e)
			}
			c := <-accepted
			defer c.Close()
			g.Stop()
			st.SetDeadline(time.Now().Add(time.Second))
			var b [1]byte
			if _, e := st.Read(b[:]); e == nil {
				t.Fatal("flow survived closed tunnel")
			}
		})
	}
}
