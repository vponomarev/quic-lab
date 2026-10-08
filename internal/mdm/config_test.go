package mdm

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const validDocument = `{"schema":1,"profiles":[{"id":"default","name":"Main","settings":{"transport":"quic","endpoint":"vpn.example:443"}}],"currentProfileId":"default","enabledProfileIds":["default"],"multiple":false,"globalApps":[],"dns":{"mode":"tunnel","profileId":"default"},"budget":{"limitBytes":0},"diagnostics":{"enabled":true,"detailed":false}}`

func TestConfigValidation(t *testing.T) {
	if err := ValidateDocument(json.RawMessage(validDocument)); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"unknown schema":    strings.Replace(validDocument, "\"schema\":1", "\"schema\":2", 1),
		"unknown root":      strings.Replace(validDocument, "\"schema\":1", "\"shell\":\"id\",\"schema\":1", 1),
		"missing schema":    strings.Replace(validDocument, "\"schema\":1,", "", 1),
		"null":              "null",
		"missing profiles":  strings.Replace(validDocument, "\"profiles\"", "\"other\"", 1),
		"unknown current":   strings.Replace(validDocument, "\"currentProfileId\":\"default\"", "\"currentProfileId\":\"missing\"", 1),
		"unknown enabled":   strings.Replace(validDocument, "\"enabledProfileIds\":[\"default\"]", "\"enabledProfileIds\":[\"missing\"]", 1),
		"duplicate enabled": strings.Replace(validDocument, "\"enabledProfileIds\":[\"default\"]", "\"enabledProfileIds\":[\"default\",\"default\"]", 1),
		"traversal id":      strings.ReplaceAll(validDocument, "\"default\"", "\"../keys\""),
		"extra settings":    strings.Replace(validDocument, "\"transport\":\"quic\"", "\"transport\":\"quic\",\"mdm_active\":true", 1),
		"type coercion":     strings.Replace(validDocument, "\"multiple\":false", "\"multiple\":\"false\"", 1),
		"null boolean":      strings.Replace(validDocument, "\"multiple\":false", "\"multiple\":null", 1),
		"negative budget":   strings.Replace(validDocument, "\"limitBytes\":0", "\"limitBytes\":-1", 1),
		"fractional pool":   strings.Replace(validDocument, "\"transport\":\"quic\"", "\"transport\":\"quic\",\"quic_pool_size\":1.5", 1),
		"huge pool":         strings.Replace(validDocument, "\"transport\":\"quic\"", "\"transport\":\"quic\",\"quic_pool_size\":1000", 1),
		"unknown transport": strings.Replace(validDocument, "\"quic\"", "\"shell\"", 1),
		"invalid endpoint":  strings.Replace(validDocument, "vpn.example:443", "https://vpn.example/run", 1),
		"duplicate key":     strings.Replace(validDocument, "\"schema\":1", "\"schema\":2,\"schema\":1", 1),
		"extra document":    validDocument + "{}",
		"oversize":          strings.Repeat(" ", 1<<20) + validDocument,
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDocument(json.RawMessage(raw)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("wanted ErrInvalid, got %v", err)
			}
		})
	}
}
func TestConfigRoutingSettings(t *testing.T) {
	raw := strings.Replace(validDocument, `"transport":"quic"`, `"transport":"quic","mode":3,"routes":"10.0.0.0/8\n192.168.0.0/16","bond_copy_kib":256,"cell_mib":100`, 1)
	if err := ValidateDocument(json.RawMessage(raw)); err != nil {
		t.Fatal("valid native settings rejected", err)
	}
	for _, pair := range [][2]string{{`"mode":3`, `"mode":4`}, {`10.0.0.0/8`, `not-a-route`}, {`"bond_copy_kib":256`, `"bond_copy_kib":65537`}} {
		if err := ValidateDocument(json.RawMessage(strings.Replace(raw, pair[0], pair[1], 1))); err == nil {
			t.Fatal("invalid routing setting accepted")
		}
	}
}
func TestConfigInvalidDoesNotChangeRevision(t *testing.T) {
	s, b, _ := fixture(t)
	if _, err := s.SetDesired(b.ID, 0, "current", json.RawMessage(validDocument)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDesired(b.ID, 1, "external", json.RawMessage(`{"schema":1}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid accepted: %v", err)
	}
	r, err := s.SetDesired(b.ID, 1, "external", json.RawMessage(validDocument))
	if err != nil || r.Revision != 2 {
		t.Fatalf("invalid changed revision: %+v %v", r, err)
	}
	if _, err = s.SetDesired(b.ID, 1, "current", json.RawMessage(validDocument)); !errors.Is(err, ErrConflict) {
		t.Fatalf("lost CAS: %v", err)
	}
}
func TestConfigurationReserve(t *testing.T) {
	raw := strings.Replace(validDocument, `"schema":1`, `"schema":1,"reserve":{"wifi_on":true,"wifi_off":true,"cell_on":false,"cell_off":false,"metered_wifi":false}`, 1)
	if e := ValidateDocument(json.RawMessage(raw)); e != nil {
		t.Fatal(e)
	}
	bad := strings.Replace(raw, `"wifi_on":true`, `"wifi_on":"true"`, 1)
	if ValidateDocument(json.RawMessage(bad)) == nil {
		t.Fatal("coercion")
	}
	bad = strings.Replace(raw, `"wifi_on":true`, `"shell":true`, 1)
	if ValidateDocument(json.RawMessage(bad)) == nil {
		t.Fatal("unknown reserve")
	}
}
