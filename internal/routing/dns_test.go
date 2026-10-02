package routing

import (
	"net/netip"
	"testing"
)

func TestDNSPolicyValidation(t *testing.T) {
	for _, p := range []DNSPolicy{{Mode: "tunnel", ExitID: "a"}, {Mode: "system", Servers: []netip.Addr{netip.MustParseAddr("192.0.2.53")}}} {
		if err := ValidateDNS(p, []string{"a"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []DNSPolicy{{Mode: "direct"}, {Mode: "tunnel", ExitID: "missing"}, {Mode: "system", Servers: []netip.Addr{netip.MustParseAddr("::1")}}, {Mode: "system", ExitID: "a"}} {
		if ValidateDNS(p, []string{"a"}) == nil {
			t.Fatalf("accepted invalid policy %+v", p)
		}
	}
}
