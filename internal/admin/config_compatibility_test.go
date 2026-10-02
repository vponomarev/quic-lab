package admin

import (
	"encoding/json"
	"testing"
)

func TestVPNProfileCarriesCompatibilityAndTLSIdentity(t *testing.T) {
	var p Profile
	err := json.Unmarshal([]byte(`{"hostname":"real.test","server_name":"cover.test","verify_name":"real.test","control_url":"https://real.test/api/v1/capabilities","data_version":1}`), &p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	json.Unmarshal(b, &fields)
	for key, want := range map[string]string{"server_name": "cover.test", "verify_name": "real.test", "control_url": "https://real.test/api/v1/capabilities"} {
		if fields[key] != want {
			t.Fatalf("profile lost %s: %s", key, b)
		}
	}
}

func TestExportedVPNProfileAlwaysPreflights(t *testing.T) {
	w := &Web{Config: Config{PublicURL: "https://lab.example/admin/", VPN: Profile{Hostname: "real.test"}}}
	p, err := w.profile(User{Name: "test", Protocols: []string{"quic"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.ControlURL != "https://lab.example/api/v1/capabilities" || p.ServerName != "real.test" || p.VerifyName != "real.test" || p.DataVersion != 1 {
		t.Fatalf("missing profile defaults: %+v", p)
	}
	w.Config.VPN.ServerName = "cover.test"
	if _, err := w.profile(User{Protocols: []string{"quic"}}); err == nil {
		t.Fatal("cover profile without explicit verified name exported")
	}
}
