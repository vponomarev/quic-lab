package linuxclient

import (
	"encoding/json"
	"strings"
	"testing"
)

const testAWG = "[Interface]\nPrivateKey=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=\nAddress=10.55.0.2/32\nDNS=192.0.2.2\n[Peer]\nPublicKey=AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=\nEndpoint=vpn.example:51820\nAllowedIPs=192.0.2.0/25,192.0.2.128/25\n"
const testVLESS = "vless://01234567-89ab-cdef-0123-456789abcdef@vpn.example:443?security=tls&sni=secure.example&type=ws&path=%2Ftunnel&host=cover.example"

func TestStandaloneConfigurationAndProjection(t *testing.T) {
	for _, kind := range []string{"awg", "vless"} {
		c := Config{Transport: kind, Endpoint: "198.18.0.2:443", Targets: []string{"192.0.2.0/24"}}
		if kind == "awg" {
			c.AWGConfig = testAWG
		} else {
			c.VLESSURI = testVLESS
		}
		raw, _ := json.Marshal(c)
		parsed, e := Decode(raw)
		if e != nil {
			t.Fatal(kind, e)
		}
		projected, e := parsed.GatewayJSON()
		if e != nil {
			t.Fatal(e)
		}
		var out map[string]any
		json.Unmarshal([]byte(projected), &out)
		if out["endpoint"] != c.Endpoint || out[kind+"_config"] == nil {
			t.Fatal("missing engine config")
		}
		for _, field := range []string{"targets", "interface", "vless_uri", "certificate", "key"} {
			if _, ok := out[field]; ok {
				t.Fatal("unrelated field in engine payload", field)
			}
		}
		if kind == "vless" && !strings.Contains(projected, "WebSocketPath") {
			t.Fatal("WS lost")
		}
	}
}
func TestStandaloneRejectsUnsafeOptionsBeforeNetwork(t *testing.T) {
	base := Config{Transport: "awg", Endpoint: "198.18.0.2:443", Targets: []string{"192.0.2.0/24"}, AWGConfig: testAWG}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.AWGConfig = strings.Replace(testAWG, "192.0.2.128/25", "192.0.2.128/26", 1) },
		func(c *Config) { c.VLESSURI = testVLESS },
		func(c *Config) { c.Key = "synthetic-secret" },
		func(c *Config) { c.DNS = "not-an-ip" },
		func(c *Config) { c.Transport = "quic" },
		func(c *Config) {
			c.Transport = "vless"
			c.AWGConfig = ""
			c.VLESSURI = testVLESS
			c.CA = "synthetic-secret"
		},
		func(c *Config) { c.Transport = "vless"; c.AWGConfig = ""; c.VLESSURI = "{}" },
		func(c *Config) { c.Targets = []string{"198.18.0.0/24"} },
		func(c *Config) { c.Targets = []string{"0.0.0.0/0"} },
	} {
		c := base
		mutate(&c)
		e := c.Validate()
		if e == nil {
			t.Fatal("unsafe config accepted")
		}
		if strings.Contains(e.Error(), "synthetic-secret") {
			t.Fatal("credential leak")
		}
		if _, e = c.GatewayJSON(); e == nil {
			t.Fatal("invalid projection accepted")
		}
	}
}
