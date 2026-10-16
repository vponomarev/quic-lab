package mobile

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"net/http"
	"quiclab/internal/bond"
	"quiclab/internal/gateway"
	"quiclab/internal/vless"
	"sync/atomic"
	"testing"
	"time"
)

func TestBondVLESSSharedExitContinuity(t *testing.T) {
	for _, blackhole := range []bool{false, true} {
		name := "reset"
		if blackhole {
			name = "blackhole"
		}
		t.Run(name, func(t *testing.T) { testBondVLESSSharedExitContinuity(t, blackhole) })
	}
}
func testBondVLESSSharedExitContinuity(t *testing.T, blackhole bool) {
	pair, cp, kp := testIdentity(t)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(cp))
	ln, err := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	srv, _ := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	hs := &http.Server{Handler: http.HandlerFunc(srv.ServeBondHTTPS)}
	defer hs.Close()
	go hs.Serve(ln)
	dst, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	var accepts atomic.Int32
	go func() {
		for {
			c, e := dst.Accept()
			if e != nil {
				return
			}
			accepts.Add(1)
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	g := NewGateway(nil)
	defer g.Stop()
	g.ctx, g.cancel = context.WithCancel(context.Background())
	g.cfg = gatewayConfig{Transport: "https", Hostname: "localhost"}
	g.tls = &tls.Config{RootCAs: roots, ServerName: "localhost", Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
	g.bond = bond.NewMux(g.ctx, true)
	g.bondToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	join := func(id string, create bool, generation uint64) func() {
		t.Helper()
		slot, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		addr := slot.Addr().String()
		slot.Close()
		const uuid = "11111111-1111-4111-8111-111111111111"
		proxy, e := vless.StartServer(context.Background(), vless.ServerOptions{Listen: addr, Security: "tls", Mode: "standalone", Certificate: []byte(cp), Key: []byte(kp), Clients: []vless.ServerClient{{UUID: uuid}}})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { proxy.Close() })
		drop := &atomic.Bool{}
		client, e := vless.New(context.Background(), vless.Config{Endpoint: addr, UUID: uuid, Security: "tls", ServerName: "localhost", RootPEM: []byte(cp)}, relayFaultDialer{drop: drop})
		if e != nil {
			t.Fatal(e)
		}
		info := bond.PathInfo{ID: id, ProfileID: id, Network: "wifi", Generation: generation}
		if e = g.addBondHTTPSWithDial(context.Background(), ln.Addr().String(), info, nil, create, client.DialContext, func() { client.Close() }); e != nil {
			t.Fatal(e)
		}
		if e = g.bond.Session.ProbeData(id); e != nil {
			t.Fatal(e)
		}
		awaitRuntime(t, func() bool {
			for _, p := range g.bond.Session.Stats().Paths {
				if p.Name == id && p.DataProbes > 0 {
					return true
				}
			}
			return false
		})
		return func() {
			if blackhole {
				drop.Store(true)
			} else {
				proxy.Close()
			}
		}
	}
	stopPrimary := join("primary", true, 1)
	stopReserve := join("reserve", false, 1)
	token := g.bondToken
	mux := g.bond
	stream, err := g.dialStream(context.Background(), "tcp", dst.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	exchange := func(seed byte) {
		t.Helper()
		stream.SetDeadline(time.Now().Add(15 * time.Second))
		payload := bytes.Repeat([]byte{seed}, 128*1024)
		sent := make(chan error, 1)
		go func() { _, e := stream.Write(payload); sent <- e }()
		got := make([]byte, len(payload))
		_, e := io.ReadFull(stream, got)
		if e != nil {
			t.Fatal(e)
		}
		if e = <-sent; e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(got, payload) {
			t.Fatal("payload changed")
		}
	}
	g.bond.Session.PreferPath("primary")
	exchange(1)
	stopPrimary()
	started := time.Now()
	exchange(2)
	pause := time.Since(started)
	if pause > 3*time.Second {
		t.Fatal("warm failover exceeded 3 seconds")
	}
	stopReserve()
	stream.SetDeadline(time.Now().Add(20 * time.Second))
	pending := []byte("queued while all carriers unavailable")
	if _, err := stream.Write(pending); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Second)
	join("replacement", false, 1)
	got := make([]byte, len(pending))
	if _, err := io.ReadFull(stream, got); err != nil || !bytes.Equal(got, pending) {
		t.Fatal("pending payload lost", err)
	}
	exchange(3)
	if g.bond != mux || g.bondToken != token || accepts.Load() != 1 {
		t.Fatal("session or destination socket replaced", accepts.Load())
	}
	t.Logf("accepts=%d warm_failover_ms=%d outage_seconds=10 payload_exact=true session_unchanged=true", accepts.Load(), pause.Milliseconds())
}

type relayFaultDialer struct{ drop *atomic.Bool }

func (d relayFaultDialer) DialContext(ctx context.Context, n, a string) (net.Conn, error) {
	c, e := (&net.Dialer{}).DialContext(ctx, n, a)
	if e != nil {
		return nil, e
	}
	return &relayFaultConn{Conn: c, drop: d.drop}, nil
}

type relayFaultConn struct {
	net.Conn
	drop *atomic.Bool
}

func (c *relayFaultConn) Write(p []byte) (int, error) {
	if c.drop.Load() {
		return len(p), nil
	}
	return c.Conn.Write(p)
}
