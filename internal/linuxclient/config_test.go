package linuxclient

import (
	"encoding/json"
	"quiclab/internal/exitcontrol"
	"strings"
	"testing"
)

func TestConfigSafety(t *testing.T) {
	good := Config{Transport: "quic", Endpoint: "198.18.0.2:443", Targets: []string{"192.0.2.2/32"}, Interface: "eth0"}
	if e := good.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, targets := range [][]string{{}, {"0.0.0.0/0", "192.0.2.0/24"}, {"198.18.0.0/24"}, {"192.0.2.1/24"}, {"127.0.0.0/8"}, {"::/0"}, {"192.0.2.0/24", "192.0.2.2/32"}, {"10.254.254.2/32"}} {
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

func TestFullIPv4Configuration(t *testing.T) {
	for _, transport := range []string{"quic", "https", "awg", "vless"} {
		c := Config{Transport: transport, Endpoint: "198.18.0.2:443", Targets: []string{"0.0.0.0/0"}}
		if transport == "awg" {
			c.AWGConfig = strings.Replace(testAWG, "192.0.2.0/25,192.0.2.128/25", "0.0.0.0/0", 1)
			c.HealthDNS = "8.8.8.8"
		}
		if transport == "vless" {
			c.VLESSURI = testVLESS
			c.HealthDNS = "8.8.8.8"
		}
		if err := c.Validate(); err != nil {
			t.Fatalf("%s: %v", transport, err)
		}
	}
}

func TestFullIPv4PreservesSpecificRoutes(t *testing.T) {
	raw := []byte(`[{"dst":"default","dev":"eth0","metric":600},{"dst":"192.168.5.0/24","dev":"eth0"},{"dst":"172.17.0.0/16","dev":"docker0"}]`)
	if err := CheckRoutes([]string{"0.0.0.0/0"}, raw); err != nil {
		t.Fatal(err)
	}
	if CheckRoutes([]string{"0.0.0.0/0"}, []byte(`[{"dst":"default","table":100}]`)) == nil {
		t.Fatal("foreign table accepted")
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

func TestServerControlCredentialsValidation(t *testing.T) {
	c := Config{Transport: "quic", Endpoint: "198.18.0.2:443", Targets: []string{"192.0.2.2/32"}, ServerControl: &exitcontrol.Config{ConfigURL: "https://control.example/api/v1/devices/device/config", DeviceID: "device", Token: strings.Repeat("a", 64)}}
	raw, _ := json.Marshal(c)
	if _, err := Decode(raw); err != nil {
		t.Fatal(err)
	}
	c.ServerControl.Token = ""
	if c.Validate() == nil {
		t.Fatal("partial control identity accepted")
	}
}
