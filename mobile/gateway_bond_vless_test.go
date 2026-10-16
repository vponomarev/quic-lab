package mobile

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"quiclab/internal/bond"
	"quiclab/internal/gateway"
	"quiclab/internal/protocol"
	"quiclab/internal/vless"
	"sync/atomic"
	"testing"
	"time"
)

func TestBondInjectedDialKeepsExitSocket(t *testing.T) {
	for _, realProxy := range []bool{false, true} {
		name := "injected"
		if realProxy {
			name = "vless"
		}
		t.Run(name, func(t *testing.T) { testBondInjectedDialKeepsExitSocket(t, realProxy) })
	}
}
func testBondInjectedDialKeepsExitSocket(t *testing.T, realProxy bool) {
	pair, cp, kp := testIdentity(t)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(cp))
	ln, err := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	server, _ := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	hs := &http.Server{Handler: http.HandlerFunc(server.ServeBondHTTPS)}
	defer hs.Close()
	go hs.Serve(ln)
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	var accepted atomic.Int32
	go func() {
		for {
			c, e := target.Accept()
			if e != nil {
				return
			}
			accepted.Add(1)
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	g := NewGateway(nil)
	defer g.Stop()
	raw, _ := json.Marshal(gatewayConfig{Transport: "https", Endpoint: ln.Addr().String(), Hostname: "localhost", CA: cp, Certificate: cp, Key: kp, MaxAvailability: true})
	if err = g.Start(string(raw), nil); err != nil {
		t.Fatal(err)
	}
	stream, err := g.dialStream(context.Background(), "tcp", target.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(8 * time.Second))
	check := func(payload string) {
		t.Helper()
		if _, e := stream.Write([]byte(payload)); e != nil {
			t.Fatal(e)
		}
		b := make([]byte, len(payload))
		if _, e := io.ReadFull(stream, b); e != nil || string(b) != payload {
			t.Fatal("payload", e)
		}
	}
	check("before")
	var calls, cleaned atomic.Int32
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		calls.Add(1)
		if address != "unreachable.invalid:443" {
			return nil, errors.New("wrong relay target")
		}
		return (&net.Dialer{}).DialContext(ctx, network, ln.Addr().String())
	}
	if realProxy {
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
		defer proxy.Close()
		client, e := vless.New(context.Background(), vless.Config{Endpoint: addr, UUID: uuid, Security: "tls", ServerName: "localhost", RootPEM: []byte(cp)}, &net.Dialer{})
		if e != nil {
			t.Fatal(e)
		}
		defer client.Close()
		dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			calls.Add(1)
			if address != "unreachable.invalid:443" {
				return nil, errors.New("wrong relay target")
			}
			return client.DialContext(ctx, network, ln.Addr().String())
		}
	}
	info := bond.PathInfo{ID: "relay-reserve", ProfileID: "relay", Network: "wifi", Generation: 1}
	if err = g.addBondHTTPSWithDial(context.Background(), "unreachable.invalid:443", info, nil, false, dial, func() { cleaned.Add(1) }); err != nil {
		t.Fatal(err)
	}
	g.DropBondPath("wifi")
	check("after")
	if accepted.Load() != 1 || calls.Load() != 1 {
		t.Fatal("new destination or unintended dial", accepted.Load(), calls.Load())
	}
	g.bond.Session.RemoveNamedPath(info)
	if cleaned.Load() != 1 {
		t.Fatal("cleanup", cleaned.Load())
	}
}
func TestBondInjectedDialFailureCleansOnce(t *testing.T) {
	g := NewGateway(nil)
	g.tls = &tls.Config{}
	g.cfg.Hostname = "localhost"
	var cleaned atomic.Int32
	sentinel := errors.New("blocked")
	err := g.addBondHTTPSWithDial(context.Background(), "blocked.invalid:443", bond.PathInfo{}, nil, true, func(context.Context, string, string) (net.Conn, error) { return nil, sentinel }, func() { cleaned.Add(1) })
	if err == nil || cleaned.Load() != 1 {
		t.Fatal("failure cleanup", err, cleaned.Load())
	}
}

func TestBondRelayCapabilitiesUseInjectedDial(t *testing.T) {
	pair, cp, _ := testIdentity(t)
	var redirect atomic.Bool
	caps := protocol.CapabilitiesHandler(protocol.Capabilities{ControlVersion: 1, DataVersion: 1})
	h := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if redirect.Load() {
			http.Redirect(w, r, "https://elsewhere.invalid/", 302)
			return
		}
		caps.ServeHTTP(w, r)
	}))
	h.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
	h.StartTLS()
	defer h.Close()
	g := NewGateway(nil)
	g.cfg = gatewayConfig{ControlURL: "https://localhost:1/api/v1/capabilities", CA: cp, DataVersion: 1}
	var calls atomic.Int32
	dial := func(ctx context.Context, n, a string) (net.Conn, error) {
		calls.Add(1)
		if a != "localhost:1" {
			t.Fatal("control target", a)
		}
		return (&net.Dialer{}).DialContext(ctx, n, h.Listener.Addr().String())
	}
	if err := g.checkCapabilitiesWithDial(dial); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("dial count", calls.Load())
	}
	redirect.Store(true)
	if err := g.checkCapabilitiesWithDial(dial); err == nil {
		t.Fatal("redirect accepted")
	}
}
func TestBondVLESSDialerClosedAndValidation(t *testing.T) {
	var cfg gatewayConfig
	json.Unmarshal([]byte(vlessGatewayJSON(t)), &cfg)
	dial, closePath, err := newBondVLESSDialer(cfg.VLESSConfig, cfg.Endpoint, gatewayVLESSBinder{})
	if err != nil {
		t.Fatal(err)
	}
	closePath()
	closePath()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err = dial(ctx, "tcp4", "example.com:443"); err == nil {
		t.Fatal("closed engine dial succeeded")
	}
	if _, _, err = newBondVLESSDialer("{}", cfg.Endpoint, gatewayVLESSBinder{}); err == nil {
		t.Fatal("invalid profile accepted")
	}
}

// A usable underlay must never bypass authentication of the internal gateway.
func TestBondRelayWrongInnerIdentity(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	h := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unauthenticated request reached handler") }))
	h.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	h.StartTLS()
	defer h.Close()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(cp))
	g := NewGateway(nil)
	g.cfg = gatewayConfig{Hostname: "wrong.invalid", Certificate: cp, Key: kp}
	g.tls = &tls.Config{RootCAs: roots, ServerName: "wrong.invalid", Certificates: []tls.Certificate{pair}}
	var cleaned atomic.Int32
	err := g.addBondHTTPSWithDial(context.Background(), "wrong.invalid:443", bond.PathInfo{}, nil, true, func(ctx context.Context, n, a string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, n, h.Listener.Addr().String())
	}, func() { cleaned.Add(1) })
	if err == nil || cleaned.Load() != 1 {
		t.Fatal("identity rejection/cleanup", err, cleaned.Load())
	}
}
func TestBondRelayCanceledHandshakeCleansOnce(t *testing.T) {
	g := NewGateway(nil)
	g.cfg.Hostname = "localhost"
	g.tls = &tls.Config{ServerName: "localhost"}
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	done := make(chan error, 1)
	var cleaned atomic.Int32
	go func() {
		done <- g.addBondHTTPSWithDial(ctx, "localhost:443", bond.PathInfo{}, nil, true, func(ctx context.Context, n, a string) (net.Conn, error) {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}, func() { cleaned.Add(1) })
	}()
	<-entered
	cancel()
	select {
	case err := <-done:
		if err == nil || cleaned.Load() != 1 {
			t.Fatal("cancel/cleanup", err, cleaned.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("handshake ignored cancellation")
	}
}
