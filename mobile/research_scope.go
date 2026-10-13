package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"quiclab/internal/gateway"
	"quiclab/internal/research"
	"reflect"
)

type clientResearchScope struct {
	Mode          string  `json:"mode"`
	DirectAllowed bool    `json:"direct_allowed"`
	UIDs          []int64 `json:"uids"`
}

func (s *clientResearchScope) allows(uid int64) bool {
	if s == nil || !s.DirectAllowed {
		return false
	}
	if s.Mode == "all_apps" {
		return true
	}
	if s.Mode == "vpn_plus_apps" && uid >= 0 {
		for _, u := range s.UIDs {
			if u == uid {
				return true
			}
		}
	}
	return false
}
func (m *MultiRouter) SetResearchScope(raw string) error {
	var s clientResearchScope
	if len(raw) > 65536 || json.Unmarshal([]byte(raw), &s) != nil {
		return errors.New("invalid research scope")
	}
	if s.Mode != "vpn_only" && s.Mode != "vpn_plus_apps" && s.Mode != "all_apps" {
		return errors.New("invalid research mode")
	}
	if len(s.UIDs) > 2048 {
		return errors.New("too many research UIDs")
	}
	for _, u := range s.UIDs {
		if u < 0 {
			return errors.New("invalid research UID")
		}
	}
	if reflect.DeepEqual(m.researchScope.Load(), &s) {
		return nil
	}
	m.mu.Lock()
	p := m.profiles["direct"]
	m.mu.Unlock()
	var c *researchCollector
	if p != nil {
		p.h.g.mu.Lock()
		c = p.h.g.research
		p.h.g.mu.Unlock()
	}
	if c != nil {
		c.reset()
	}
	m.researchScope.Store(&s)
	if s.DirectAllowed && s.Mode != "vpn_only" {
		m.directResearchDNS.Store(true)
	}
	if c != nil {
		c.changed()
	}
	return nil
}

type directResearchObserver struct {
	router   *MultiRouter
	delegate ResearchObserver
}

func (o *directResearchObserver) Allowed() bool {
	s := o.router.researchScope.Load()
	return s != nil && s.DirectAllowed && s.Mode != "vpn_only" && o.delegate.Allowed()
}
func (o *directResearchObserver) OwnerForUID(uid int64) string {
	if !o.Allowed() {
		return ""
	}
	return o.delegate.OwnerForUID(uid)
}
func (o *directResearchObserver) Owner(p int64, l string, lp int64, r string, rp int64) string {
	if !o.Allowed() {
		return ""
	}
	return o.delegate.Owner(p, l, lp, r, rp)
}
func configureDirectCollector(c *researchCollector, m *MultiRouter) {
	c.exit = research.ExitRef{Route: "direct", ID: "direct", Type: "direct"}
	c.lookupUID = m.owner
	c.eligible = func(f *researchFlow) bool { return m.researchScope.Load().allows(f.uid) }
}

// SetResearch installs the direct observer. Tunnel collectors retain their
// authenticated sessions; direct records use one selected tunnel for delivery.
func (m *MultiRouter) SetResearch(observer ResearchObserver) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	p := m.profiles["direct"]
	m.researchObserver = observer
	if p == nil {
		m.mu.Unlock()
		return
	}
	g := p.h.g
	g.mu.Lock()
	old := g.research
	g.research = nil
	var c *researchCollector
	if observer != nil {
		c = newResearchCollector(&directResearchObserver{m, observer}, m.exchangeDirectResearch)
		if c != nil {
			configureDirectCollector(c, m)
			g.research = c
		}
	}
	g.mu.Unlock()
	m.mu.Unlock()
	if old != nil {
		old.stop()
	}
	if c != nil {
		c.changed()
		go c.run()
	}
}
func (m *MultiRouter) exchangeDirectResearch(ctx context.Context, b gateway.ResearchBatch) (gateway.ResearchReply, error) {
	m.mu.Lock()
	var selected *Gateway
	// Pin diagnostics to one configured account. Never fail over metadata to a
	// different profile/server merely because the first transport is unavailable.
	id := m.policy.Rules[0].ID
	if p := m.profiles[id]; p != nil && !m.disabled[id] {
		p.h.g.mu.Lock()
		kind := p.h.g.cfg.Transport
		p.h.g.mu.Unlock()
		if kind == "quic" || kind == "https" {
			selected = p.h.g
		}
	}

	m.mu.Unlock()
	if selected == nil {
		return gateway.ResearchReply{}, errors.New("no research delivery tunnel")
	}
	return selected.exchangeResearch(ctx, b)
}
