package linuxclient

import (
	"encoding/json"
	"testing"
)

func TestConfigSafety(t *testing.T) {
	good := Config{Transport: "quic", Endpoint: "198.18.0.2:443", Targets: []string{"192.0.2.2/32"}, Interface: "eth0"}
	if e := good.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, targets := range [][]string{{}, {"0.0.0.0/0"}, {"198.18.0.0/24"}, {"192.0.2.1/24"}, {"127.0.0.0/8"}, {"::/0"}, {"192.0.2.0/24", "192.0.2.2/32"}, {"10.254.254.2/32"}} {
		c := good
		c.Targets = targets
		if c.Validate() == nil {
			t.Fatalf("accepted %v", targets)
		}
	}
	c := good
	c.Interface = "eth0;touch /tmp/x"
	if c.Validate() == nil {
		t.Fatal("shell-like interface")
	}
	c = good
	c.Transport = "vless"
	if c.Validate() == nil {
		t.Fatal("unverified transport")
	}
	raw, _ := json.Marshal(good)
	if _, e := Decode(raw); e != nil {
		t.Fatal(e)
	}
}
func TestRouteConflicts(t *testing.T) {
	targets := []string{"192.0.2.2/32"}
	for _, raw := range []string{`[{"dst":"192.0.2.2","dev":"eth0"}]`, `[{"dst":"192.0.2.0/24","dev":"eth0"}]`, `[{"dst":"default","table":"100"}]`} {
		if CheckRoutes(targets, []byte(raw)) == nil {
			t.Fatal("accepted conflicting route", raw)
		}
	}
	if e := CheckRoutes(targets, []byte(`[{"dst":"default","dev":"eth0"},{"dst":"198.18.0.0/30","dev":"eth0"}]`)); e != nil {
		t.Fatal(e)
	}
}
