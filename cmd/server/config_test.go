package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServerConfigFileAndOverrides(t *testing.T) {
	t.Setenv("CREDENTIALS_DIRECTORY", "/run/test")
	p := filepath.Join(t.TempDir(), "server.json")
	os.WriteFile(p, []byte(`{"listen":"0.0.0.0:14433","cert":"${CREDENTIALS_DIRECTORY}/cert.pem","key":"${CREDENTIALS_DIRECTORY}/key.pem","https_listen":"0.0.0.0:443","tls_host":"lab.example.org","tls_fallback":"192.0.2.10:443"}`), 0600)
	c, e := parseServerConfig([]string{"-config", p, "-listen", "127.0.0.1:4433"})
	if e != nil {
		t.Fatal(e)
	}
	if c.Listen != "127.0.0.1:4433" || c.Cert != "/run/test/cert.pem" || c.TLSFallback != "192.0.2.10:443" {
		t.Fatalf("unexpected config: %+v", c)
	}
}
func TestServerConfigRejectsInvalid(t *testing.T) {
	for _, data := range []string{`{"ephemeral_cert":true,"unknown":1}`, `{"ephemeral_cert":true} {}`, `{"ephemeral_cert":true,"listen":"0.0.0.0:0"}`, `{"ephemeral_cert":true,"tls_fallback":"192.0.2.10:443"}`, `{"ephemeral_cert":true,"https_listen":":443","tls_host":"lab.test","tls_fallback":"0.0.0.0:9443"}`, `{"ephemeral_cert":true,"gateway_quic":":4434","gateway_allow":"::/0"}`} {
		p := filepath.Join(t.TempDir(), "server.json")
		os.WriteFile(p, []byte(data), 0600)
		if _, err := parseServerConfig([]string{"-config", p}); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}
func TestServerConfigLegacyCLI(t *testing.T) {
	if _, err := parseServerConfig([]string{"-ephemeral-cert", "-listen", "127.0.0.1:4433"}); err != nil {
		t.Fatal(err)
	}
}
func TestFallbackRemoteAddresses(t *testing.T) {
	for _, addr := range []string{"192.0.2.10:443", "web.internal.example:9443", "[2001:db8::1]:443"} {
		if err := validateAddress(addr, true); err != nil {
			t.Fatal(err)
		}
	}
}
