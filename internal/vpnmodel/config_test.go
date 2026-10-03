package vpnmodel

import (
	"encoding/json"
	"strings"
	"testing"
)

const validConfig = `{"version":1,"exits":[{"id":"home","name":"Home","kind":"standalone"},{"id":"net","name":"Internet","kind":"demux","demux_id":"server-a"}],"profiles":[{"id":"home","exit_id":"home","transport":"awg","mode":"auto","endpoint":"home.example:51820","pool_size":1},{"id":"fast","exit_id":"net","transport":"quic","mode":"auto","endpoint":"vpn.example:443","pool_size":3},{"id":"fallback","exit_id":"net","transport":"https","mode":"reserve","endpoint":"vpn.example:443","pool_size":1,"priority":1,"check_reserve":true}]}`

func TestConfigOwnership(t *testing.T) {
	c, err := Parse([]byte(validConfig))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Exits) != 2 || len(c.Profiles) != 3 || c.Profiles[2].ExitID != "net" || !c.Profiles[2].CheckReserve {
		t.Fatalf("configuration lost: %+v", c)
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Parse(raw); err != nil {
		t.Fatal(err)
	}
}
func TestInvalidConfigRejected(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"duplicate exit", func(c *Config) { c.Exits = append(c.Exits, c.Exits[0]) }},
		{"duplicate profile", func(c *Config) { c.Profiles = append(c.Profiles, c.Profiles[0]) }},
		{"missing owner", func(c *Config) { c.Profiles[0].ExitID = "missing" }},
		{"empty ID", func(c *Config) { c.Exits[0].ID = "" }},
		{"unsafe ID", func(c *Config) { c.Profiles[0].ID = "../identity" }},
		{"unknown version", func(c *Config) { c.Version = 2 }},
		{"unknown exit kind", func(c *Config) { c.Exits[0].Kind = "mystery" }},
		{"AWG demux not phase one", func(c *Config) { c.Profiles[1].Transport = "awg" }},
		{"second standalone profile", func(c *Config) { p := c.Profiles[0]; p.ID = "extra"; c.Profiles = append(c.Profiles, p) }},
		{"pool below range", func(c *Config) { c.Profiles[1].PoolSize = 0 }},
		{"pool above range", func(c *Config) { c.Profiles[1].PoolSize = 6 }},
		{"AWG carousel unsupported", func(c *Config) { c.Profiles[0].PoolSize = 2 }},
		{"unknown mode", func(c *Config) { c.Profiles[1].Mode = "fallback-maybe" }},
		{"unknown transport", func(c *Config) { c.Profiles[1].Transport = "vless" }},
		{"missing demux identity", func(c *Config) { c.Exits[1].DemuxID = "" }},
		{"invalid port", func(c *Config) { c.Profiles[1].Endpoint = "host:70000" }},
		{"negative priority", func(c *Config) { c.Profiles[1].Priority = -1 }},
		{"orphan exit", func(c *Config) { c.Profiles = c.Profiles[1:] }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var c Config
			if err := json.Unmarshal([]byte(validConfig), &c); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&c)
			if err := Validate(c); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
func TestParseRejectsAmbiguousInput(t *testing.T) {
	for _, raw := range []string{validConfig + " {}", "null", strings.Replace(validConfig, `"version":1`, `"version":1,"key":"secret"`, 1), `{"version":1,"exits":[],"profiles":[]}`} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Fatal("ambiguous or empty input accepted")
		}
	}
}
func TestDisabledDraftCanBeMigrated(t *testing.T) {
	raw := `{"version":1,"exits":[{"id":"default","name":"Draft","kind":"standalone"}],"profiles":[{"id":"default","exit_id":"default","transport":"quic","mode":"disabled","endpoint":"","pool_size":1}]}`
	if _, err := Parse([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}

func TestVLESSStandaloneOnly(t *testing.T) {
	var c Config
	if err := json.Unmarshal([]byte(validConfig), &c); err != nil {
		t.Fatal(err)
	}
	c.Profiles[0].Transport = "vless"
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	c.Profiles[0].PoolSize = 2
	if err := Validate(c); err == nil {
		t.Fatal("VLESS carousel accepted")
	}
	c.Profiles[0].PoolSize = 1
	c.Profiles[1].Transport = "vless"
	if err := Validate(c); err == nil {
		t.Fatal("VLESS demux accepted before V3")
	}
}
