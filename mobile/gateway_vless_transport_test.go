package mobile

import (
	"encoding/json"
	"quiclab/internal/vless"
	"testing"
)

func TestStoredVLESSPreservesWebSocket(t *testing.T) {
	p, e := vless.ParseImport("vless://01234567-89ab-cdef-0123-456789abcdef@vpn.example:443?security=tls&sni=secure.example&type=ws&path=%2Ftunnel%3Fed%3D2048&host=cover.example")
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(p)
	c, e := decodeGatewayVLESS(string(raw), "192.0.2.1:443")
	if e != nil {
		t.Fatal(e)
	}
	if c.Transport != "ws" || c.WebSocketPath != "/tunnel?ed=2048" || c.WebSocketHost != "cover.example" || c.ServerName != "secure.example" || c.Endpoint != "192.0.2.1:443" {
		t.Fatal("stored WebSocket profile changed transport/options")
	}
}
func TestStoredVLESSRejectsDiscardedInvalidOptions(t *testing.T) {
	p, e := vless.ParseImport("vless://01234567-89ab-cdef-0123-456789abcdef@vpn.example:443?security=tls&sni=secure.example")
	if e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*vless.Config){
		func(c *vless.Config) { c.WebSocketPath = "/ignored" },
		func(c *vless.Config) { c.Transport = "grpc" },
		func(c *vless.Config) { c.Transport = "ws"; c.Flow = "xtls-rprx-vision" },
	} {
		bad := p
		mutate(&bad.Config)
		raw, _ := json.Marshal(bad)
		if _, e := decodeGatewayVLESS(string(raw), "192.0.2.1:443"); e == nil {
			t.Fatal("invalid stored options silently discarded")
		}
	}
	p.Config.Transport = ""
	raw, _ := json.Marshal(p)
	if _, e := decodeGatewayVLESS(string(raw), "192.0.2.1:443"); e != nil {
		t.Fatal("legacy TCP rejected", e)
	}
}
