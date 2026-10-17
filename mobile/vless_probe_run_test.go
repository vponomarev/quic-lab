package mobile

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"quiclab/internal/gateway"
	"quiclab/internal/probeecho"
	"quiclab/internal/vless"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type probeTestClient struct{}

func (probeTestClient) DialContext(c context.Context, n, a string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(c, n, a)
}
func (probeTestClient) Close() error { return nil }
func TestVLESSProbeRun(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.Handler
		want    string
	}{
		{"echo", probeecho.NewHandler(), "success"},
		{"wrong nonce", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"version":1,"nonce":"wrong"}`)) }), "unexpected_response"},
		{"redirect", http.RedirectHandler("https://example.invalid/", 302), "unexpected_response"},
		{"oversized", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(strings.Repeat("x", 1025))) }), "unexpected_response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(tc.handler)
			defer srv.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &VLESSProbe{ctx: ctx, cancel: cancel, client: probeTestClient{}}
			raw, _ := json.Marshal(map[string]any{"target": srv.URL})
			result, e := p.run(string(raw), srv.Client().Transport.(*http.Transport).TLSClientConfig)
			if e != nil {
				t.Fatal(e)
			}
			var v probeRunResult
			json.Unmarshal([]byte(result), &v)
			if v.Outcome != tc.want {
				t.Fatalf("got %+v", v)
			}
			if tc.want == "success" && v.EchoSucceeded != 3 {
				t.Fatalf("echo count %+v", v)
			}
			bad, e := p.run(string(raw), &tls.Config{})
			if e != nil {
				t.Fatal(e)
			}
			json.Unmarshal([]byte(bad), &v)
			if v.Outcome != "target_tls" {
				t.Fatalf("untrusted TLS: %s", bad)
			}
		})
	}
}

func TestVLESSProbeRealCarrierRelayAndCancel(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(cp))
	slot, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := slot.Addr().String()
	slot.Close()
	const uuid = "11111111-1111-4111-8111-111111111111"
	server, e := vless.StartServer(context.Background(), vless.ServerOptions{Listen: addr, Security: "tls", Mode: "standalone", Certificate: []byte(cp), Key: []byte(kp), Clients: []vless.ServerClient{{UUID: uuid}}})
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observer := &probeSocketObserver{factory: relayFaultDialer{drop: &atomic.Bool{}}}
	client, e := vless.New(ctx, vless.Config{Endpoint: addr, UUID: uuid, Security: "tls", ServerName: "localhost", RootPEM: []byte(cp)}, observer)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	p := &VLESSProbe{ctx: ctx, cancel: cancel, client: client, observer: observer}
	echo := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ip" {
			io.WriteString(w, `{"ip":"203.0.113.42"}`)
			return
		}
		probeecho.NewHandler().ServeHTTP(w, r)
	}))
	echo.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	echo.StartTLS()
	defer echo.Close()
	p.exitIPURL = echo.URL + "/ip"
	p.exitIPTLS = &tls.Config{RootCAs: roots}
	gw, _ := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	inner := httptest.NewUnstartedServer(http.HandlerFunc(gw.ServeBondHTTPS))
	inner.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13}
	inner.StartTLS()
	defer inner.Close()
	options := map[string]any{"target": echo.URL, "ca": cp, "gateway": gatewayConfig{Transport: "https", Endpoint: inner.Listener.Addr().String(), Hostname: "localhost", Certificate: cp, Key: kp, CA: cp}}
	raw, _ := json.Marshal(options)
	result, e := p.Run(string(raw))
	if e != nil {
		t.Fatal(e)
	}
	var v probeRunResult
	json.Unmarshal([]byte(result), &v)
	if v.Outcome != "success" || v.RelayOutcome != "success" || v.EchoSucceeded != 3 || v.ExitIP != "203.0.113.42" || v.ExitIPOutcome != "success" || v.TCPOutcome != "success" {
		t.Fatal(result)
	}
	options["gateway"] = gatewayConfig{Endpoint: inner.Listener.Addr().String(), Hostname: "wrong.example", Certificate: cp, Key: kp, CA: cp}
	raw, _ = json.Marshal(options)
	result, e = p.Run(string(raw))
	if e != nil {
		t.Fatal(e)
	}
	json.Unmarshal([]byte(result), &v)
	if v.RelayOutcome != "relay_auth" {
		t.Fatalf("certificate mismatch must be authentication failure: %s", result)
	}
	options["gateway"] = gatewayConfig{Endpoint: inner.Listener.Addr().String(), Hostname: "localhost"}
	raw, _ = json.Marshal(options)
	result, e = p.run(string(raw), &tls.Config{RootCAs: roots, ServerName: "localhost"})
	if e != nil {
		t.Fatal(e)
	}
	json.Unmarshal([]byte(result), &v)
	if v.EchoSucceeded != 3 || v.RelayOutcome != "relay_auth" {
		t.Fatal(result)
	}
	stalled := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
	stalled.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	stalled.StartTLS()
	defer stalled.Close()
	raw, _ = json.Marshal(map[string]any{"target": stalled.URL})
	done := make(chan string, 1)
	go func() { s, _ := p.run(string(raw), &tls.Config{RootCAs: roots, ServerName: "localhost"}); done <- s }()
	time.Sleep(100 * time.Millisecond)
	p.Close()
	select {
	case s := <-done:
		json.Unmarshal([]byte(s), &v)
		if v.Outcome != "canceled" {
			t.Fatal(s)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel")
	}
}

func TestProbeStageReport(t *testing.T) {
	srv := httptest.NewTLSServer(probeecho.NewHandler())
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &VLESSProbe{ctx: ctx, cancel: cancel, client: probeTestClient{}}
	raw, _ := json.Marshal(map[string]any{"target": srv.URL})
	result, err := p.run(string(raw), srv.Client().Transport.(*http.Transport).TLSClientConfig)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	json.Unmarshal([]byte(result), &fields)
	if fields["httpsOutcome"] != "success" || fields["proxyOutcome"] != "confirmed" {
		t.Fatalf("missing stage evidence: %s", result)
	}
}

func TestProbeIndependentIP(t *testing.T) {
	for _, test := range []struct{ name, body, echoWant, ipWant string }{
		{"echo unavailable", `{"ip":"203.0.113.8"}`, "unexpected_response", "success"},
		{"malformed IP", `{"ip":"not-an-ip"}`, "success", "unexpected_response"},
		{"private IP", `{"ip":"192.168.1.1"}`, "success", "unexpected_response"},
		{"oversized IP", strings.Repeat("x", 1025), "success", "unexpected_response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/ip" {
					io.WriteString(w, test.body)
					return
				}
				if test.echoWant != "success" {
					w.WriteHeader(503)
					return
				}
				probeecho.NewHandler().ServeHTTP(w, r)
			}))
			defer srv.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &VLESSProbe{ctx: ctx, cancel: cancel, client: probeTestClient{}, exitIPURL: srv.URL + "/ip", exitIPTLS: srv.Client().Transport.(*http.Transport).TLSClientConfig}
			raw, _ := json.Marshal(map[string]any{"target": srv.URL})
			out, err := p.run(string(raw), srv.Client().Transport.(*http.Transport).TLSClientConfig)
			if err != nil {
				t.Fatal(err)
			}
			var result probeRunResult
			json.Unmarshal([]byte(out), &result)
			if result.HTTPSOutcome != test.echoWant || result.ExitIPOutcome != test.ipWant {
				t.Fatal(out)
			}
			if test.ipWant != "success" && result.ExitIP != "" {
				t.Fatal("invalid IP retained")
			}
		})
	}
}
