package routing

import (
	"net/netip"
	"testing"
)

func TestPriorityUnknownAndOverlap(t *testing.T) {
	p, e := Parse(`[{"id":"home","mode":"subnets","subnets":["192.168.50.9/24"]},{"id":"apps","mode":"apps","uids":[10001]}]`)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct {
		ip   string
		uid  int64
		want string
	}{{"192.168.50.8", 10001, "home"}, {"192.168.50.8", -1, "home"}, {"1.1.1.1", 10001, "apps"}, {"1.1.1.1", 10002, "direct"}, {"1.1.1.1", -1, ""}} {
		if got := p.Choose(netip.MustParseAddr(v.ip), v.uid); got.Profile != v.want {
			t.Fatal(v, got)
		}
	}
	reverse, _ := Parse(`[{"id":"apps","mode":"apps","uids":[10001]},{"id":"home","mode":"subnets","subnets":["192.168.50.0/24"]}]`)
	if reverse.Choose(netip.MustParseAddr("192.168.50.8"), 10001).Profile != "apps" {
		t.Fatal("priority ignored")
	}
}
func TestInvalidRules(t *testing.T) {
	for _, raw := range []string{`[]`, `[{"id":"direct","mode":"all"}]`, `[{"id":"a","mode":"apps"}]`, `[{"id":"a","mode":"subnets","subnets":["::/0"]}]`, `[{"id":"a","mode":"all"},{"id":"a","mode":"all"}]`} {
		if _, e := Parse(raw); e == nil {
			t.Fatal(raw)
		}
	}
}
func TestExcludeAndDefault(t *testing.T) {
	p, _ := Parse(`[{"id":"a","mode":"exclude","uids":[7]},{"id":"b","mode":"all"}]`)
	ip := netip.MustParseAddr("1.2.3.4")
	if p.Choose(ip, 7).Profile != "b" || p.Choose(ip, 8).Profile != "a" || p.Choose(ip, -1).Profile != "" {
		t.Fatal("exclude policy")
	}
}
