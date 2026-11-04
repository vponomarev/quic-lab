package linuxclient

import (
	"encoding/json"
	"quiclab/internal/approuting"
	"testing"
)

func TestLinuxApplicationConfig(t *testing.T) {
	c := Config{Transport: "quic", Endpoint: "198.18.0.2:443", Targets: []string{"192.0.2.2/32"}, AppRouting: &approuting.TUNPolicy{Version: 2, Enabled: true, Mode: approuting.OnlySelected, Only: []string{"/opt/app/Game"}, Except: []string{"/usr/bin/browser"}}}
	raw, _ := json.Marshal(c)
	loaded, e := Decode(raw)
	if e != nil || loaded.AppRouting == nil || !loaded.AppRouting.Enabled {
		t.Fatal(e)
	}
	loaded.AppRouting.Version = 1
	if loaded.Validate() == nil {
		t.Fatal("legacy WFP policy accepted")
	}
	loaded.AppRouting.Version = 2
	loaded.AppRouting.Unknown = "guess"
	if loaded.Validate() == nil {
		t.Fatal("bad unknown policy")
	}
	loaded.AppRouting.Unknown = "tunnel"
	loaded.AppRouting.Only = []string{`C:\Windows\app.exe`}
	if loaded.Validate() == nil {
		t.Fatal("Windows path accepted")
	}
}
