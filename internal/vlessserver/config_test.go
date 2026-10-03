package vlessserver

import (
	"crypto/ecdh"
	"encoding/base64"
	"net/url"
	"quiclab/internal/vless"
	"strings"
	"testing"
)

func tlsConfig() Config {
	return Config{Listen: "0.0.0.0:8443", Endpoint: "vpn.example:8443", Security: "tls", ServerName: "vpn.example", Fingerprint: "chrome", TLSCertificateFile: "/etc/quic-lab/tls.crt", TLSKeyFile: "/etc/quic-lab/tls.key", Mode: "standalone"}
}
func realityConfig() Config {
	c := tlsConfig()
	c.Security = "reality"
	c.TLSCertificateFile = ""
	c.TLSKeyFile = ""
	c.RealityPrivateKey = base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	c.RealityTarget = "cover.example:443"
	c.RealityServerNames = []string{"cover.example"}
	c.ServerName = "cover.example"
	c.RealityShortIDs = []string{"aabb"}
	return c
}
func TestConfigRejectsInvalidSecurityAndDestination(t *testing.T) {
	for _, base := range []Config{tlsConfig(), realityConfig()} {
		if err := base.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	changes := []func(*Config){
		func(c *Config) { c.Listen = "localhost:8443" }, func(c *Config) { c.Listen = "[::]:8443" }, func(c *Config) { c.Endpoint = "vpn.example:0" },
		func(c *Config) { c.Endpoint = "[::1]:8443" }, func(c *Config) { c.Security = "none" }, func(c *Config) { c.Mode = "other" }, func(c *Config) { c.Mode = "demux-only" },
		func(c *Config) { c.Flow = "secret-invalid" }, func(c *Config) { c.RealityPrivateKey = "secret-private" }, func(c *Config) { c.TLSKeyFile = "relative.key" },
		func(c *Config) { c.Mode = "standalone"; c.DemuxEndpoint = "127.0.0.1:443" }, func(c *Config) { c.Mode = "demux-only"; c.DemuxEndpoint = "[::1]:443" },
	}
	for i, change := range changes {
		c := tlsConfig()
		change(&c)
		err := c.Validate()
		if err == nil {
			t.Fatalf("case %d accepted", i)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("secret leaked")
		}
	}
	for i, change := range []func(*Config){func(c *Config) { c.ServerName = "other.example" }, func(c *Config) { c.RealityPrivateKey = "bad" }, func(c *Config) { c.RealityShortIDs = []string{"zz"} }, func(c *Config) { c.RealityShortIDs = []string{"aabb", "aabb00"} }, func(c *Config) { c.RealityTarget = "bad" }, func(c *Config) { c.TLSKeyFile = "/key" }, func(c *Config) { c.RealityServerNames = nil }, func(c *Config) { c.RealityShortIDs = nil }} {
		c := realityConfig()
		change(&c)
		if c.Validate() == nil {
			t.Fatalf("REALITY case %d accepted", i)
		}
	}
	c := tlsConfig()
	c.Mode = "demux-only"
	c.DemuxEndpoint = "127.0.0.1:8444"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestClientURIRoundTrips(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	for _, c := range []Config{tlsConfig(), realityConfig()} {
		for _, flow := range []string{"", "xtls-rprx-vision"} {
			c.Flow = flow
			raw, err := c.ClientURI(id, "Телефон / office & home")
			if err != nil {
				t.Fatal(err)
			}
			p, err := vless.ParseImport(raw)
			if err != nil {
				t.Fatal(err)
			}
			if p.Config.UUID != id || p.Config.Endpoint != c.Endpoint || p.Config.ServerName != c.ServerName || p.Config.Flow != flow || p.Name != "Телефон / office & home" {
				t.Fatal("round trip changed profile")
			}
			if c.Security == "reality" {
				key, _ := base64.RawURLEncoding.DecodeString(c.RealityPrivateKey)
				priv, _ := ecdh.X25519().NewPrivateKey(key)
				if p.Config.RealityPublicKey != base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()) {
					t.Fatal("wrong public key")
				}
				if strings.Contains(raw, c.RealityPrivateKey) {
					t.Fatal("private key exported")
				}
			}
			u, _ := url.Parse(raw)
			if u.Query().Get("security") != c.Security {
				t.Fatal("wrong security")
			}
		}
	}
	c := tlsConfig()
	if _, err := c.ClientURI("secret-invalid", "n"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("invalid UUID or secret error")
	}
}
