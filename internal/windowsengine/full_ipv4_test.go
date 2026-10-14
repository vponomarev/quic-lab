package windowsengine

import (
	"net/netip"
	"testing"
)

func TestFullIPv4RoutePlan(t *testing.T) {
	routes, e := FullIPv4Routes(netip.MustParseAddr("45.9.88.105"), []string{"192.168.5.0/24"})
	if e != nil {
		t.Fatal(e)
	}
	contains := func(ip string) bool {
		a := netip.MustParseAddr(ip)
		for _, s := range routes {
			if netip.MustParsePrefix(s).Contains(a) {
				return true
			}
		}
		return false
	}
	for _, ip := range []string{"1.1.1.1", "8.8.8.8", "10.0.0.1", "192.168.6.1", "45.9.88.106", "223.255.255.254"} {
		if !contains(ip) {
			t.Fatal("missing", ip)
		}
	}
	for _, ip := range []string{"45.9.88.105", "192.168.5.1", "127.0.0.1", "169.254.1.1", "224.0.0.1", "10.254.253.1", "0.0.0.0"} {
		if contains(ip) {
			t.Fatal("must bypass", ip)
		}
	}
	if validateTargets(routes, netip.MustParseAddr("45.9.88.105")) != nil {
		t.Fatal("invalid effective routes")
	}
}
func TestRoutingModeCompatibility(t *testing.T) {
	s := valid()
	if s.Validate() != nil {
		t.Fatal("legacy broken")
	}
	s.RoutingMode = "all_ipv4"
	s.UplinkMode = "auto"
	s.InterfaceIndex = 0
	s.Targets = nil
	if s.Validate() != nil {
		t.Fatal("full automatic rejected")
	}
	s.RoutingMode = "invalid"
	if s.Validate() == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestFullIPv4ProbeScope(t *testing.T) {
	s := valid()
	s.RoutingMode = "all_ipv4"
	s.Targets = nil
	if err := s.ValidateProbe("168.119.242.240"); err != nil {
		t.Fatal(err)
	}
}
