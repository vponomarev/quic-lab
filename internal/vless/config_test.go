package vless

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/xtls/xray-core/app/proxyman"
	account "github.com/xtls/xray-core/proxy/vless"
	outbound "github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/transport/internet/reality"
	xtls "github.com/xtls/xray-core/transport/internet/tls"
)

func validConfig() Config {
	return Config{Endpoint: "outer.invalid:443", UUID: "11111111-1111-4111-8111-111111111111", Security: "tls", ServerName: "server.invalid", Fingerprint: "chrome"}
}
func TestBuildConfigRestrictsRuntime(t *testing.T) {
	cfg, err := buildConfig(validConfig())
	if err != nil {
		t.Fatal("valid config rejected")
	}
	if len(cfg.Inbound) != 0 || len(cfg.Outbound) != 1 || len(cfg.App) != 2 {
		t.Fatal("unexpected runtime scope")
	}
	raw, err := cfg.Outbound[0].SenderSettings.GetInstance()
	if err != nil {
		t.Fatal("sender unavailable")
	}
	sender := raw.(*proxyman.SenderConfig)
	if sender.ProxySettings != nil || sender.MultiplexSettings != nil || sender.TargetStrategy != 0 || sender.Via != nil {
		t.Fatal("unexpected outbound behavior")
	}
	stream := sender.StreamSettings
	if stream.ProtocolName != "tcp" || stream.SocketSettings != nil || len(stream.SecuritySettings) != 1 {
		t.Fatal("unexpected transport behavior")
	}
	sec, err := stream.SecuritySettings[0].GetInstance()
	if err != nil {
		t.Fatal("security unavailable")
	}
	tls := sec.(*xtls.Config)
	if tls.ServerName != "server.invalid" || tls.Fingerprint != "chrome" || tls.AllowInsecure || tls.EchConfigList != "" || tls.MasterKeyLog != "" {
		t.Fatal("unexpected TLS behavior")
	}
	raw, err = cfg.Outbound[0].ProxySettings.GetInstance()
	if err != nil {
		t.Fatal("outbound unavailable")
	}
	v := raw.(*outbound.Config)
	if v.Vnext.Address.AsAddress().Domain() != "outer.invalid" || v.Vnext.Port != 443 {
		t.Fatal("endpoint changed")
	}
	raw, err = v.Vnext.User.Account.GetInstance()
	if err != nil {
		t.Fatal("account unavailable")
	}
	a := raw.(*account.Account)
	if a.Encryption != "none" || a.Testpre != 0 || a.Reverse != nil {
		t.Fatal("unexpected account behavior")
	}
}
func TestBuildConfigRealityAndVision(t *testing.T) {
	c := validConfig()
	c.Security = "reality"
	c.Flow = "xtls-rprx-vision"
	key := bytes.Repeat([]byte{7}, 32)
	c.RealityPublicKey = base64.RawURLEncoding.EncodeToString(key)
	c.ShortID = "abcd"
	cfg, err := buildConfig(c)
	if err != nil {
		t.Fatal("valid REALITY config rejected")
	}
	raw, _ := cfg.Outbound[0].SenderSettings.GetInstance()
	s := raw.(*proxyman.SenderConfig)
	raw, _ = s.StreamSettings.SecuritySettings[0].GetInstance()
	r := raw.(*reality.Config)
	if !bytes.Equal(r.PublicKey, key) || len(r.ShortId) != 8 || r.ShortId[0] != 0xab || r.ShortId[1] != 0xcd || r.ServerName != c.ServerName || r.Fingerprint != "chrome" {
		t.Fatal("REALITY values changed")
	}
	raw, _ = cfg.Outbound[0].ProxySettings.GetInstance()
	raw, _ = raw.(*outbound.Config).Vnext.User.Account.GetInstance()
	if raw.(*account.Account).Flow != c.Flow {
		t.Fatal("Vision flow lost")
	}
}
func TestBuildConfigPrivateTLSRoot(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("fixture key generation failed")
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Unix(0, 0), NotAfter: time.Unix(4102444800, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, pub, key)
	if err != nil {
		t.Fatal("fixture certificate generation failed")
	}
	c := validConfig()
	c.RootPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	cfg, err := buildConfig(c)
	if err != nil {
		t.Fatal("fixture root rejected")
	}
	raw, _ := cfg.Outbound[0].SenderSettings.GetInstance()
	s := raw.(*proxyman.SenderConfig)
	raw, _ = s.StreamSettings.SecuritySettings[0].GetInstance()
	tls := raw.(*xtls.Config)
	if !tls.DisableSystemRoot || len(tls.Certificate) != 1 || tls.Certificate[0].Usage != xtls.Certificate_AUTHORITY_VERIFY || !tls.Certificate[0].OneTimeLoading || !bytes.Equal(tls.Certificate[0].Certificate, c.RootPEM) {
		t.Fatal("fixture trust root changed")
	}
}
func TestBuildConfigRejectsUnsupportedInputs(t *testing.T) {
	cases := map[string]func(*Config){
		"missing endpoint": func(c *Config) { c.Endpoint = "" }, "missing port": func(c *Config) { c.Endpoint = "outer.invalid" }, "zero port": func(c *Config) { c.Endpoint = "outer.invalid:0" }, "large port": func(c *Config) { c.Endpoint = "outer.invalid:65536" }, "ipv6": func(c *Config) { c.Endpoint = "[::1]:443" }, "url endpoint": func(c *Config) { c.Endpoint = "https://outer.invalid:443" },
		"invalid uuid": func(c *Config) { c.UUID = "secret-invalid-uuid" }, "uuid alias": func(c *Config) { c.UUID = "device-name" }, "security": func(c *Config) { c.Security = "none" }, "flow": func(c *Config) { c.Flow = "secret-unsupported-flow" }, "fingerprint": func(c *Config) { c.Fingerprint = "secret-unsupported-fingerprint" }, "missing name": func(c *Config) { c.ServerName = "" }, "name URL": func(c *Config) { c.ServerName = "https://server.invalid" },
		"special name": func(c *Config) { c.ServerName = "frommitm" }, "TLS reality key": func(c *Config) { c.RealityPublicKey = "secret-key" }, "TLS shortid": func(c *Config) { c.ShortID = "ab" }, "invalid root": func(c *Config) { c.RootPEM = []byte("secret-not-a-cert") },
		"reality missing key": func(c *Config) { c.Security = "reality" }, "reality short key": func(c *Config) {
			c.Security = "reality"
			c.RealityPublicKey = base64.RawURLEncoding.EncodeToString([]byte{1})
		},
		"reality invalid shortid": func(c *Config) {
			c.Security = "reality"
			c.RealityPublicKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
			c.ShortID = "secret-invalid-shortid"
		},
		"reality long shortid": func(c *Config) {
			c.Security = "reality"
			c.RealityPublicKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
			c.ShortID = "001122334455667788"
		},
		"reality root": func(c *Config) { c.Security = "reality"; c.RootPEM = []byte("secret-root") }, "reality no fingerprint": func(c *Config) {
			c.Security = "reality"
			c.RealityPublicKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
			c.Fingerprint = ""
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := validConfig()
			mutate(&c)
			cfg, err := buildConfig(c)
			if err == nil || cfg != nil {
				t.Fatal("invalid config accepted")
			}
			if strings.Contains(err.Error(), "secret-") {
				t.Fatal("validation leaked supplied value")
			}
		})
	}
}

func TestSpiderXPathValidation(t *testing.T) {
	for _, value := range []string{"", "/", "/docs/a%20b?q=x%2Fy&lang=ru"} {
		got, err := NormalizeSpiderX(value)
		if err != nil || got == "" {
			t.Fatalf("valid path %q: %v", value, err)
		}
	}
	for _, value := range []string{"https://host/", "//host/", "/a#fragment", "/a%0d%0a", "/bad%xx", "/a\n", "/a\\b"} {
		if _, err := NormalizeSpiderX(value); err == nil {
			t.Fatalf("unsafe path accepted %q", value)
		}
	}
}
