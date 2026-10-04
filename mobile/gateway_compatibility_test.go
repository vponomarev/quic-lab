package mobile

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"github.com/quic-go/quic-go"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"quiclab/internal/gateway"
	"quiclab/internal/protocol"
	"strings"
	"testing"
	"time"
)

func TestGatewayTLSNames(t *testing.T) {
	for _, test := range []struct {
		host, sni, verify, wantSNI, wantVerify string
		reject                                 bool
	}{
		{host: "real.test", wantSNI: "real.test", wantVerify: "real.test"},
		{host: "real.test", sni: "cover.test", verify: "real.test", wantSNI: "cover.test", wantVerify: "real.test"},
		{host: "real.test", sni: "cover.test", reject: true},
	} {
		cfg := gatewayConfig{Hostname: test.host, ServerName: test.sni, VerifyName: test.verify}
		sni, verify, err := cfg.tlsNames()
		if test.reject {
			if err == nil {
				t.Fatal("cover without explicit verification identity accepted")
			}
			continue
		}
		if err != nil || sni != test.wantSNI || verify != test.wantVerify {
			t.Fatalf("names %q %q %v", sni, verify, err)
		}
	}
}

func TestIncompatibleExit(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(cp))
	tc := &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tc)
	if err != nil {
		t.Fatal(err)
	}
	backend, _ := gateway.New("127.0.0.0/8", slog.Default())
	hs := &http.Server{Handler: http.HandlerFunc(backend.WebSocket)}
	defer hs.Close()
	go hs.Serve(ln)
	home := NewGateway(nil)
	defer home.Stop()
	raw, _ := json.Marshal(gatewayConfig{Transport: "https", Endpoint: ln.Addr().String(), Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
	if err := home.Start(string(raw), nil); err != nil {
		t.Fatal(err)
	}
	caps := httptest.NewUnstartedServer(protocol.CapabilitiesHandler(protocol.Capabilities{ControlVersion: 1, DataVersion: 9}))
	caps.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
	caps.StartTLS()
	defer caps.Close()
	internet := NewGateway(nil)
	defer internet.Stop()
	raw, _ = json.Marshal(gatewayConfig{Transport: "quic", Endpoint: "127.0.0.1:1", Hostname: "localhost", Certificate: cp, Key: kp, CA: cp, ControlURL: caps.URL + "/api/v1/capabilities", AndroidVersionCode: 45})
	if err := internet.Start(string(raw), nil); !errors.Is(err, protocol.ErrUpgradeRequired) {
		t.Fatalf("internet: %v", err)
	}
	if !home.IsConnected() {
		t.Fatal("incompatible internet exit stopped home")
	}
	stream, err := home.open(context.Background())
	if err != nil {
		t.Fatalf("home unusable: %v", err)
	}
	stream.Close()
}
func TestGatewayChecksCapabilitiesBeforeDataHandshake(t *testing.T) {
	caps := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/capabilities" {
			t.Errorf("control path %q", r.URL.Path)
		}
		if len(r.TLS.PeerCertificates) != 0 || r.Header.Get("Authorization") != "" {
			t.Error("public capabilities sent secret credentials")
		}
		protocol.CapabilitiesHandler(protocol.Capabilities{ControlVersion: 1, DataVersion: 9}).ServeHTTP(w, r)
	}))
	caps.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	caps.StartTLS()
	defer caps.Close()
	pair := caps.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg := gatewayConfig{Transport: "quic", Endpoint: "127.0.0.1:1", Hostname: "example.com", Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]})), Key: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})), ControlURL: caps.URL + "/api/v1/capabilities", AndroidVersionCode: 45, CA: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]}))}
	data, _ := json.Marshal(cfg)
	g := NewGateway(nil)
	defer g.Stop()
	if err := g.Start(string(data), nil); !errors.Is(err, protocol.ErrUpgradeRequired) {
		t.Fatalf("data connection attempted before capabilities: %v", err)
	}
}

func TestGatewayQUICAndHTTPSCustomSNI(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	caps := httptest.NewUnstartedServer(protocol.CapabilitiesHandler(protocol.Capabilities{ControlVersion: 1, DataVersion: protocol.DataVersion}))
	caps.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
	caps.StartTLS()
	defer caps.Close()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(cp))
	for _, mode := range []string{"quic", "https"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			seen := make(chan string, 1)
			tc := &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, NextProtos: []string{gateway.ALPN}, GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) { seen <- h.ServerName; return nil, nil }}
			srv, _ := gateway.New("127.0.0.0/8", slog.Default())
			var endpoint string
			if mode == "quic" {
				ln, err := quic.ListenAddr("127.0.0.1:0", tc, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				endpoint = ln.Addr().String()
				go srv.ServeQUIC(ctx, ln)
			} else {
				tc.NextProtos = []string{"http/1.1"}
				ln, err := tls.Listen("tcp", "127.0.0.1:0", tc)
				if err != nil {
					t.Fatal(err)
				}
				endpoint = ln.Addr().String()
				hs := &http.Server{Handler: http.HandlerFunc(srv.WebSocket)}
				defer hs.Close()
				go hs.Serve(ln)
			}
			g := NewGateway(nil)
			defer g.Stop()
			raw, _ := json.Marshal(gatewayConfig{Transport: mode, Endpoint: endpoint, Hostname: "localhost", ServerName: "cover.test", VerifyName: "localhost", Certificate: cp, Key: kp, CA: cp, ControlURL: caps.URL, AndroidVersionCode: 45})
			if err := g.Start(string(raw), nil); err != nil {
				t.Fatal(err)
			}
			select {
			case name := <-seen:
				if name != "cover.test" {
					t.Fatalf("SNI %q", name)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("no TLS hello")
			}
			for attempt := 0; attempt < 3; attempt++ {
				if err := g.Reconnect(nil); err != nil {
					t.Fatalf("reconnect %d: %v", attempt, err)
				}
				select {
				case <-seen:
				case <-time.After(3 * time.Second):
					t.Fatal("no reconnect TLS hello")
				}
				if !g.IsConnected() {
					t.Fatal("replacement transport disconnected")
				}
				stream, err := g.open(context.Background())
				if err != nil {
					t.Fatalf("replacement stream: %v", err)
				}
				stream.Close()
			}
		})
	}
}

func TestSeparateControlIdentity(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	caps := httptest.NewUnstartedServer(protocol.CapabilitiesHandler(protocol.Capabilities{ControlVersion: 1, DataVersion: 9}))
	caps.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
	caps.StartTLS()
	defer caps.Close()
	raw, _ := json.Marshal(gatewayConfig{Transport: "quic", Endpoint: "127.0.0.1:1", Hostname: "vpn.other.test", ServerName: "cover.test", VerifyName: "vpn.other.test", Certificate: cp, Key: kp, CA: cp, ControlURL: caps.URL + "/api/v1/capabilities", AndroidVersionCode: 45})
	g := NewGateway(nil)
	defer g.Stop()
	if err := g.Start(string(raw), nil); !errors.Is(err, protocol.ErrUpgradeRequired) {
		t.Fatalf("control identity coupled to VPN identity: %v", err)
	}
}

func TestControlRejectsWrongURLIdentity(t *testing.T) {
	caps := httptest.NewUnstartedServer(protocol.CapabilitiesHandler(protocol.Capabilities{ControlVersion: 1, DataVersion: 9}))
	caps.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	caps.StartTLS()
	defer caps.Close()
	pair := caps.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(gatewayConfig{Transport: "quic", Endpoint: "127.0.0.1:1", Hostname: "example.com", Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]})), Key: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})), CA: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]})), ControlURL: strings.Replace(caps.URL, "127.0.0.1", "localhost", 1) + "/api/v1/capabilities", AndroidVersionCode: 45})
	g := NewGateway(nil)
	defer g.Stop()
	var nameError x509.HostnameError
	if err := g.Start(string(raw), nil); !errors.As(err, &nameError) {
		t.Fatalf("wrong control URL identity accepted: %v", err)
	}
}

func TestProfileAndCapabilitiesMismatchEmitUpgradeRequired(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	for _, test := range []struct {
		name                    string
		profileData, serverData int
		upgrade                 bool
	}{{"profile", 9, 1, true}, {"capabilities", 1, 9, true}, {"compatible", 1, 1, false}} {
		t.Run(test.name, func(t *testing.T) {
			caps := httptest.NewUnstartedServer(protocol.CapabilitiesHandler(protocol.Capabilities{ControlVersion: 1, DataVersion: test.serverData, MinAndroidVersionCode: 45}))
			caps.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
			caps.StartTLS()
			defer caps.Close()
			sink := &eventSink{ch: make(chan map[string]any, 4)}
			g := NewGateway(sink)
			defer g.Stop()
			g.cfg = gatewayConfig{CA: cp, Certificate: cp, Key: kp, ControlURL: caps.URL + "/api/v1/capabilities", AndroidVersionCode: 45, DataVersion: test.profileData}
			err := g.checkCapabilities(nil)
			if !test.upgrade {
				if err != nil {
					t.Fatal(err)
				}
				select {
				case event := <-sink.ch:
					t.Fatalf("compatible emitted event: %+v", event)
				default:
				}
				return
			}
			if !errors.Is(err, protocol.ErrUpgradeRequired) {
				t.Fatalf("upgrade error: %v", err)
			}
			select {
			case event := <-sink.ch:
				if event["event"] != "upgrade_required" || event["min_android_version_code"] != float64(45) || event["data_version"] != float64(test.serverData) {
					t.Fatalf("upgrade event: %+v", event)
				}
			case <-time.After(100 * time.Millisecond):
				t.Fatal("upgrade_required event missing")
			}
		})
	}
}

func TestGatewayReconnectRenewsCapabilitiesContext(t *testing.T) {
	caps := httptest.NewUnstartedServer(protocol.CapabilitiesHandler(protocol.Capabilities{ControlVersion: 1, DataVersion: 9}))
	caps.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	caps.StartTLS()
	defer caps.Close()
	for _, transport := range []string{"quic", "https"} {
		t.Run(transport, func(t *testing.T) {
			g := NewGateway(nil)
			defer g.Stop()
			g.cfg = gatewayConfig{Transport: transport, ControlURL: caps.URL, CA: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caps.TLS.Certificates[0].Certificate[0]}))}
			g.ctx, g.cancel = context.WithCancel(context.Background())
			for attempt := 0; attempt < 3; attempt++ {
				err := g.Reconnect(nil)
				if !errors.Is(err, protocol.ErrUpgradeRequired) {
					t.Fatalf("attempt %d: capabilities must reach server, got %v", attempt, err)
				}
				if g.cancel != nil {
					t.Fatal("failed reconnect retained running state")
				}
			}
		})
	}
}
