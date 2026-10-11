//go:build linux

package mobile

import (
	"context"
	"quiclab/internal/gateway"
	"testing"
	"time"
)

func TestResearchScopeRouteMatrix(t *testing.T) {
	owner := &multipleOwner{}
	owner.uid.Store(200)
	m, err := NewMultiRouter(`[{"id":"vpn","mode":"apps","uids":[100]}]`, "vpn", owner, &multipleBinder{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, scope := range []string{`{"mode":"vpn_only"}`, `{"mode":"vpn_plus_apps","direct_allowed":true,"uids":[200]}`, `{"mode":"all_apps","direct_allowed":true}`} {
		if err = m.SetResearchScope(scope); err != nil {
			t.Fatal(err)
		}
		if got := m.selectFlow(6, "10.0.0.1", 1000, "1.1.1.1", 443); got == nil || got.g.direct == nil {
			t.Fatal("research changed direct route")
		}
		owner.uid.Store(100)
		if m.selectFlow(6, "10.0.0.1", 1000, "1.1.1.1", 443) != nil {
			t.Fatal("missing VPN bypassed")
		}
		owner.uid.Store(-1)
		if m.selectFlow(6, "10.0.0.1", 1000, "1.1.1.1", 443) != nil {
			t.Fatal("unknown UID bypassed")
		}
		owner.uid.Store(200)
	}
	for _, raw := range []string{`{"mode":"invalid"}`, `{"mode":"all_apps","uids":[-1]}`} {
		if m.SetResearchScope(raw) == nil {
			t.Fatal("invalid scope accepted")
		}
	}
}

func TestDirectResearchConsentAndDelivery(t *testing.T) {
	owner := &multipleOwner{}
	owner.uid.Store(200)
	m, err := NewMultiRouter(`[{"id":"vpn","mode":"apps","uids":[100]}]`, "vpn", owner, &multipleBinder{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	o := &researchTestObserver{}
	o.allowed.Store(true)
	m.SetResearch(o)
	c := m.profiles["direct"].h.g.research
	if c == nil {
		t.Fatal("direct observer missing")
	}
	c.stop() // Drive a fresh collector deterministically without its timer.
	c = newResearchCollector(&directResearchObserver{router: m, delegate: o}, func(context.Context, gateway.ResearchBatch) (gateway.ResearchReply, error) {
		return gateway.ResearchReply{Allowed: true, DirectObservationVersion: 1}, nil
	})
	defer c.stop()
	configureDirectCollector(c, m)
	f := c.openFlow(6, "10.0.0.1", 1000, "1.1.1.1", 443)
	c.sync(time.Now())
	f.add(999, 0, 0, 0)
	if c.collecting() {
		t.Fatal("default collected direct")
	}
	if err = m.SetResearchScope(`{"mode":"vpn_plus_apps","direct_allowed":true,"uids":[201]}`); err != nil {
		t.Fatal(err)
	}
	c.sync(time.Now())
	f.add(999, 0, 0, 0)
	if f.up != 0 {
		t.Fatal("unselected direct application collected")
	}
	if err = m.SetResearchScope(`{"mode":"all_apps","direct_allowed":true}`); err != nil {
		t.Fatal(err)
	}
	c.reset()
	c.sync(time.Now())
	f.add(7, 0, 0, 0)
	if f.up != 7 {
		t.Fatalf("direct data absent: %d", f.up)
	}
	if err = m.SetResearchScope(`{"mode":"all_apps","direct_allowed":false}`); err != nil {
		t.Fatal(err)
	}
	f.add(999, 0, 0, 0)
	if f.up != 7 {
		t.Fatal("revocation admitted bytes")
	}
	c.changed()
	if c.collecting() {
		t.Fatal("revocation ignored")
	}
}

func TestResearchScopeDNSRoutes(t *testing.T) {
	owner := &multipleOwner{}
	owner.uid.Store(200)
	m, err := NewMultiRouter(`[{"id":"vpn","mode":"apps","uids":[100]}]`, "vpn", owner, &multipleBinder{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.SetResearchScope(`{"mode":"all_apps","direct_allowed":true}`); err != nil {
		t.Fatal(err)
	}
	if got := m.selectFlow(17, "10.0.0.1", 1000, "1.1.1.1", 53); got == nil || got.g.direct == nil {
		t.Fatal("app-owned direct DNS entered tunnel")
	}
	owner.uid.Store(-1)
	if m.selectFlow(17, "10.0.0.1", 1000, "1.1.1.1", 53) != nil {
		t.Fatal("unowned resolver bypassed explicit unavailable DNS profile")
	}
}

func TestDirectConsentRetainsAuthorizedHistory(t *testing.T) {
	o := &researchTestObserver{}
	o.allowed.Store(true)
	c := newResearchCollector(o, func(context.Context, gateway.ResearchBatch) (gateway.ResearchReply, error) {
		return gateway.ResearchReply{Allowed: true, DirectObservationVersion: 1}, nil
	})
	defer c.stop()
	c.exit.Route = "direct"
	c.exit.ID = "direct"
	c.exit.Type = "direct"
	c.sync(time.Now())
	f := c.openFlow(6, "10.0.0.1", 1000, "1.1.1.1", 443)
	f.add(73, 21, 1, 1)
	o.allowed.Store(false)
	c.changed()
	if c.collecting() {
		t.Fatal("revoked collection active")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	found := false
	for _, q := range c.queue {
		if q.record.Type == "flow_delta" {
			found = true
		}
	}
	if !found {
		t.Fatal("authorized counters were discarded on revoke")
	}
	if len(c.queue) == 0 {
		t.Fatal("authorized history lost")
	}
}

type capturedUIDObserver struct{}

func (*capturedUIDObserver) Allowed() bool                                    { return true }
func (*capturedUIDObserver) Owner(int64, string, int64, string, int64) string { return "" } // socket already closed
func (*capturedUIDObserver) OwnerForUID(uid int64) string {
	return `{"uid":"200","packages":["app.direct"],"labels":["Direct"],"attribution":"known"}`
}
func TestDirectAttributionUsesCapturedUID(t *testing.T) {
	o := &multipleOwner{}
	o.uid.Store(200)
	c := newResearchCollector(&capturedUIDObserver{}, func(context.Context, gateway.ResearchBatch) (gateway.ResearchReply, error) {
		return gateway.ResearchReply{Allowed: true}, nil
	})
	defer c.stop()
	c.lookupUID = o
	c.sync(time.Now())
	f := c.openFlow(17, "10.0.0.1", 1000, "1.1.1.1", 443)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		known := f.app.Attribution == "known"
		c.mu.Unlock()
		if known {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("captured UID was discarded when socket owner lookup became unavailable")
}

func TestResearchScopeAllRoutingModesRemainAuthoritative(t *testing.T) {
	for _, rule := range []struct {
		raw    string
		uid    int64
		addr   string
		direct bool
	}{
		{`[{"id":"vpn","mode":"all"}]`, 200, "1.1.1.1", false},
		{`[{"id":"vpn","mode":"apps","uids":[100]}]`, 100, "1.1.1.1", false},
		{`[{"id":"vpn","mode":"apps","uids":[100]}]`, 200, "1.1.1.1", true},
		{`[{"id":"vpn","mode":"apps","uids":[100]}]`, -1, "1.1.1.1", false},
		{`[{"id":"vpn","mode":"exclude","uids":[100]}]`, 100, "1.1.1.1", true},
		{`[{"id":"vpn","mode":"exclude","uids":[100]}]`, 200, "1.1.1.1", false},
		{`[{"id":"vpn","mode":"subnets","subnets":["10.0.0.0/8"]}]`, 200, "10.1.2.3", false},
		{`[{"id":"vpn","mode":"subnets","subnets":["10.0.0.0/8"]}]`, 200, "1.1.1.1", true},
	} {
		for _, scope := range []string{`{"mode":"vpn_only"}`, `{"mode":"vpn_plus_apps","direct_allowed":true,"uids":[200]}`, `{"mode":"all_apps","direct_allowed":true}`} {
			owner := &multipleOwner{}
			owner.uid.Store(rule.uid)
			m, err := NewMultiRouter(rule.raw, "vpn", owner, &multipleBinder{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = m.SetResearchScope(scope); err != nil {
				t.Fatal(err)
			}
			h := m.selectFlow(6, "10.254.254.1", 1234, rule.addr, 443)
			got := h != nil && h.g.direct != nil
			m.Close()
			if got != rule.direct {
				t.Fatalf("research changed route: %s %s uid=%d direct=%v", rule.raw, scope, rule.uid, got)
			}
		}
	}
}
