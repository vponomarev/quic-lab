package vless

import (
	"strings"
	"testing"
)

const importURI = "vless://11111111-1111-4111-8111-111111111111@outer.invalid:443?encryption=none&security=tls&type=tcp&sni=server.invalid&fp=chrome#Fixture%20profile"
const importOutbound = `{"tag":"Fixture profile","protocol":"vless","settings":{"vnext":[{"address":"outer.invalid","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none"}]}]},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"serverName":"server.invalid","fingerprint":"chrome"}}}`

func TestParseImportTLSFormats(t *testing.T) {
	for _, raw := range []string{importURI, importOutbound} {
		p, err := ParseImport(raw)
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "Fixture profile" || p.Config.Endpoint != "outer.invalid:443" || p.Config.UUID != "11111111-1111-4111-8111-111111111111" || p.Config.Security != "tls" || p.Config.ServerName != "server.invalid" || p.Config.Fingerprint != "chrome" {
			t.Fatal("incorrect imported profile")
		}
	}
}
func TestParseImportRealityVision(t *testing.T) {
	raw := "vless://11111111-1111-4111-8111-111111111111@outer.invalid:443?security=reality&type=tcp&sni=server.invalid&fp=chrome&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=aabb&flow=xtls-rprx-vision"
	p, err := ParseImport(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Config.Security != "reality" || p.Config.Flow != "xtls-rprx-vision" || p.Config.ShortID != "aabb" {
		t.Fatal("incorrect REALITY config")
	}
}
func TestParseImportRejectsUnsafeInputs(t *testing.T) {
	cases := []string{
		"", strings.Repeat("x", 16*1024+1), "https://subscription.invalid/secret", strings.Replace(importURI, "#Fixture", "&unknown=secret#Fixture", 1),
		strings.Replace(importURI, "&security=tls", "&security=tls&security=tls", 1),
		strings.Replace(importURI, "type=tcp", "type=ws", 1),
		strings.Replace(importURI, "security=tls", "security=none", 1),
		strings.Replace(importURI, "fp=chrome", "allowInsecure=1", 1),
		strings.Replace(importURI, "11111111-1111-4111-8111-111111111111", "secret-uuid", 1),
		strings.Replace(importURI, "@outer", ":password@outer", 1),
		strings.Replace(importURI, ":443?", ":0?", 1),
		strings.Replace(importURI, "?encryption", "/path?encryption", 1),
		strings.Replace(importURI, "fp=chrome", "pbk=secret-key", 1),
		strings.Replace(importURI, "fp=chrome", "fp=chrome&flow=unsupported", 1),
		strings.Replace(importOutbound, `"protocol":"vless"`, `"protocol":"vless","protocol":"vless"`, 1),
		strings.Replace(importOutbound, `"port":443`, `"port":443,"port":443`, 1),
		strings.Replace(importOutbound, `"network":"tcp"`, `"network":"tcp","sockopt":{}`, 1),
		strings.Replace(importOutbound, `"serverName":"server.invalid"`, `"serverName":"server.invalid","allowInsecure":true`, 1),
		strings.Replace(importOutbound, `"settings":`, `"proxySettings":{"tag":"other"},"settings":`, 1),
		`{"outbounds":[` + importOutbound + `]}`, importOutbound + `{}`, `null`,
		strings.Replace(importOutbound, `"users":[`, `"users":[{},`, 1),
	}
	for i, raw := range cases {
		_, err := ParseImport(raw)
		if err == nil {
			t.Fatalf("case %d accepted", i)
		}
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "11111111") {
			t.Fatalf("case %d leaked input", i)
		}
	}
}

func TestParseImportRAWAlias(t *testing.T) {
	for _, raw := range []string{strings.Replace(importURI, "type=tcp", "type=raw", 1), strings.Replace(importOutbound, `"network":"tcp"`, `"network":"raw"`, 1)} {
		if _, err := ParseImport(raw); err != nil {
			t.Fatal("RAW alias rejected")
		}
	}
}
