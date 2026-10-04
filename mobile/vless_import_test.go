package mobile

import (
	"encoding/json"
	"quiclab/internal/vless"
	"strings"
	"testing"
)

func TestImportVLESSBridgeSeparatesSecretsFromMetadata(t *testing.T) {
	raw := "vless://11111111-1111-4111-8111-111111111111@outer.invalid:443?security=tls&sni=server.invalid#Fixture"
	canonical, err := ImportVLESSConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	var p vless.ImportedProfile
	if err := json.Unmarshal([]byte(canonical), &p); err != nil {
		t.Fatal(err)
	}
	if p.Config.UUID != "11111111-1111-4111-8111-111111111111" {
		t.Fatal("storage lost identity")
	}
	metadata, err := ValidateVLESSConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(metadata, p.Config.UUID) || strings.Contains(metadata, "RealityPublicKey") {
		t.Fatal("metadata leaks credentials")
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(metadata), &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Fixture" || got["endpoint"] != "outer.invalid:443" || got["security"] != "tls" || got["flow"] != "" {
		t.Fatal("invalid metadata")
	}
	if _, err := ImportVLESSConfig("secret malformed input"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("invalid input accepted or leaked")
	}
	if _, err := ValidateVLESSConfig("secret malformed input"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("invalid validation accepted or leaked")
	}
}

func TestVLESSStoredSpiderXAndLegacy(t *testing.T) {
	base := "vless://11111111-1111-4111-8111-111111111111@outer.invalid:443?security=reality&sni=server.invalid&fp=chrome&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=aabb"
	for _, suffix := range []string{"", "&spx=%2Fdocs%2Fa%2520b%3Fq%3Dx%252Fy"} {
		raw, err := ImportVLESSConfig(base + suffix)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeGatewayVLESS(raw, "127.0.0.1:443")
		if err != nil {
			t.Fatal(err)
		}
		if suffix != "" && got.SpiderX != "/docs/a%20b?q=x%2Fy" {
			t.Fatalf("lost SpiderX: %q", got.SpiderX)
		}
		if suffix == "" && strings.Contains(raw, "SpiderX") {
			t.Fatal("legacy canonical JSON changed")
		}
	}
}
