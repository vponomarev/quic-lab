package serverexit

import (
	"encoding/json"
	"strings"
	"testing"
)

const testAWG = "[Interface]\nPrivateKey=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=\nAddress=10.55.0.2/32\nDNS=192.0.2.2\n[Peer]\nPublicKey=AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=\nEndpoint=198.18.0.2:51820\nAllowedIPs=0.0.0.0/0\n"

func testDefinitions() []Definition {
	return []Definition{{ID: "west", Name: "West", Protocol: "awg", AWGConfig: testAWG},
		{ID: "east", Name: "East", Protocol: "awg", AWGConfig: strings.ReplaceAll(testAWG, "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=", "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")}}
}

func TestCatalogMetadataAndValidation(t *testing.T) {
	defs := testDefinitions()
	c, err := NewCatalog(defs)
	if err != nil {
		t.Fatal(err)
	}
	defs[0].Name = "changed"
	meta, ok := c.Metadata("west")
	if !ok || meta.Name != "West" {
		t.Fatal("catalogue aliased caller")
	}
	raw, _ := json.Marshal(meta)
	if strings.Contains(string(raw), "Key") || strings.Contains(string(raw), "config") || strings.Contains(string(raw), "10.55") {
		t.Fatal("private provider data in metadata")
	}
	for _, bad := range []Definition{
		{ID: "../west", Name: "West", Protocol: "awg", AWGConfig: testAWG},
		{ID: "direct", Name: "Direct", Protocol: "awg", AWGConfig: testAWG},
		{ID: "west", Name: "West", Protocol: "vless", AWGConfig: testAWG},
		{ID: "west", Name: "West", Protocol: "awg", AWGConfig: "secret-invalid-config"},
		{ID: "west", Name: "West", Protocol: "awg", AWGConfig: strings.ReplaceAll(testAWG, "0.0.0.0/0", "192.0.2.0/24")},
	} {
		_, err := NewCatalog([]Definition{bad})
		if err == nil || strings.Contains(err.Error(), "secret-invalid-config") {
			t.Fatal("invalid/secret definition error", err)
		}
	}
	if _, err := NewCatalog([]Definition{testDefinitions()[0], testDefinitions()[0]}); err == nil {
		t.Fatal("duplicate ID")
	}
	dup := testDefinitions()[0]
	dup.ID = "duplicate-peer"
	if _, err := NewCatalog([]Definition{testDefinitions()[0], dup}); err == nil {
		t.Fatal("duplicate roaming AWG peer")
	}
	if _, err := NewCatalog(make([]Definition, MaxExits+1)); err == nil {
		t.Fatal("unbounded catalogue")
	}
}

func TestPolicyDefaultSelectionAndFailClosed(t *testing.T) {
	c, err := NewCatalog(testDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	p := &Policy{Allowed: []string{"west", "east"}, Default: "west"}
	if err := c.ValidatePolicy(p); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"", "west"}, {"east", "east"}} {
		id, err := c.Resolve(p, pair[0])
		if err != nil || id != pair[1] {
			t.Fatal(id, err)
		}
	}
	if id, err := c.Resolve(nil, ""); err != nil || id != "" {
		t.Fatal("legacy changed")
	}
	for _, pair := range []struct {
		p        *Policy
		selected string
	}{
		{nil, "east"}, {&Policy{}, ""}, {p, "missing"},
		{&Policy{Allowed: []string{"west"}, Default: "west"}, "east"},
		{&Policy{Allowed: []string{"missing"}, Default: "missing"}, ""},
	} {
		if id, err := c.Resolve(pair.p, pair.selected); err == nil || id != "" {
			t.Fatal("denied selection fell back", id, err)
		}
	}
	for _, bad := range []*Policy{{Allowed: []string{"west", "west"}, Default: "west"}, {Allowed: []string{"west"}, Default: "east"}, {Allowed: []string{"missing"}, Default: "missing"}} {
		if c.ValidatePolicy(bad) == nil {
			t.Fatal("bad policy")
		}
	}
	defs := testDefinitions()
	defs[1].Disabled = true
	disabled, _ := NewCatalog(defs)
	if _, err := disabled.Resolve(p, "east"); err == nil {
		t.Fatal("disabled exit selected")
	}
}
