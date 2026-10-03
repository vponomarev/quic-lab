package mobile

import (
	"context"
	"encoding/json"
	"net"
	"testing"
)

type gatewayVLESSBinder struct{}

func (gatewayVLESSBinder) Bind(int64) error { return nil }
func vlessGatewayJSON(t *testing.T) string {
	t.Helper()
	p, err := ImportVLESSConfig("vless://01234567-89ab-cdef-0123-456789abcdef@vpn.example:443?security=tls&sni=secure.example")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"transport": "vless", "endpoint": "192.0.2.1:443", "vless_config": p})
	return string(b)
}
func TestGatewayVLESSLifecycle(t *testing.T) {
	g := NewGateway(nil)
	if err := g.Start(vlessGatewayJSON(t), gatewayVLESSBinder{}); err != nil {
		t.Fatal(err)
	}
	defer g.Stop()
	if !g.IsConnected() || g.LocalAddress() != "vless" {
		t.Fatal("engine not published")
	}
	old := g.vless
	if g.datagramBackend() != old {
		t.Fatal("UDP bypasses VLESS")
	}
	if err := g.PreparePath("other", gatewayVLESSBinder{}); err == nil {
		t.Fatal("standby enabled")
	}
	if err := g.SetEndpoint("192.0.2.2:443"); err != nil {
		t.Fatal(err)
	}
	if err := g.Reconnect(gatewayVLESSBinder{}); err != nil {
		t.Fatal(err)
	}
	if g.vless == old {
		t.Fatal("engine retained across network change")
	}
	if _, err := old.DialContext(context.Background(), "tcp4", "example.com:443"); err == nil {
		t.Fatal("old generation remains live")
	}
	g.Stop()
	if g.IsConnected() || g.datagramBackend() != nil {
		t.Fatal("stopped gateway remains live")
	}
}
func TestGatewayVLESSConfigFailsClosed(t *testing.T) {
	raw := vlessGatewayJSON(t)
	var c map[string]any
	json.Unmarshal([]byte(raw), &c)
	for _, mutation := range []func(){func() { c["vless_config"] = "{}" }, func() { c["endpoint"] = "vpn.example:443" }, func() { c["max_availability"] = true }} {
		json.Unmarshal([]byte(raw), &c)
		mutation()
		b, _ := json.Marshal(c)
		g := NewGateway(nil)
		if err := g.Start(string(b), gatewayVLESSBinder{}); err == nil {
			g.Stop()
			t.Fatal("invalid VLESS configuration accepted")
		}
		if g.IsConnected() || g.datagramBackend() != nil {
			t.Fatal("invalid profile published backend")
		}
	}
}
func TestGatewayVLESSOverridePreservesSNI(t *testing.T) {
	var c gatewayConfig
	json.Unmarshal([]byte(vlessGatewayJSON(t)), &c)
	p, err := decodeGatewayVLESS(c.VLESSConfig, c.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if p.Endpoint != "192.0.2.1:443" || p.ServerName != "secure.example" {
		t.Fatal("endpoint replacement changes TLS identity")
	}
	for _, raw := range []string{`{"config":{"RootPEM":"YQ=="}}`, c.VLESSConfig + ` {}`, `{"unknown":true}`} {
		if _, err := decodeGatewayVLESS(raw, c.Endpoint); err == nil {
			t.Fatal("unsafe canonical profile accepted")
		}
	}
}

type gatewayVLESSFake struct {
	network, target string
	closed          bool
}

func (f *gatewayVLESSFake) DialContext(_ context.Context, network, target string) (net.Conn, error) {
	f.network, f.target = network, target
	a, b := net.Pipe()
	b.Close()
	return a, nil
}
func (f *gatewayVLESSFake) Close() error { f.closed = true; return nil }
func TestGatewayVLESSRoutesTCPAndFailsClosed(t *testing.T) {
	f := &gatewayVLESSFake{}
	g := NewGateway(nil)
	g.cfg.Transport = "vless"
	g.vless = f
	c, err := g.dialStream(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if f.network != "tcp4" || f.target != "example.com:443" {
		t.Fatal("TCP flow not routed through VLESS")
	}
	if g.datagramBackend() != f {
		t.Fatal("UDP not routed through VLESS")
	}
	g.direct = f
	g.Stop()
	if _, err := g.dialStream(context.Background(), "tcp", "example.com:443"); err == nil {
		t.Fatal("stopped VLESS fell back to direct")
	}
	if g.datagramBackend() != nil {
		t.Fatal("stopped VLESS UDP fell back to direct")
	}
}

func TestGatewayVLESSMigrationFailureClosesGeneration(t *testing.T) {
	g := NewGateway(nil)
	if err := g.Start(vlessGatewayJSON(t), gatewayVLESSBinder{}); err != nil {
		t.Fatal(err)
	}
	defer g.Stop()
	old := g.vless
	if err := g.MigrateTo("new-network", gatewayVLESSBinder{}); err != nil {
		t.Fatal(err)
	}
	if g.vless == old {
		t.Fatal("migration retained old engine")
	}
	if _, err := old.DialContext(context.Background(), "tcp4", "example.com:443"); err == nil {
		t.Fatal("migration left old engine live")
	}
	current := g.vless
	if err := g.Reconnect(nil); err == nil {
		t.Fatal("unbound reconnect accepted")
	}
	if g.IsConnected() || g.datagramBackend() != nil {
		t.Fatal("failed reconnect published backend")
	}
	if _, err := current.DialContext(context.Background(), "tcp4", "example.com:443"); err == nil {
		t.Fatal("failed reconnect retained previous generation")
	}
}
