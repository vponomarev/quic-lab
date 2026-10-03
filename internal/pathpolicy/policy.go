package pathpolicy

import (
	"math/rand/v2"
	"quiclab/internal/vpnmodel"
	"sort"
	"sync"
	"time"
)

type Decision struct {
	PathID, ProfileID, Network, Action, Reason string
	Generation                                 uint64
}
type candidate struct {
	profile                                                    vpnmodel.Profile
	network, id, state                                         string
	generation                                                 uint64
	health                                                     Health
	retry, started, healthy, last, progressSince, lastProgress time.Time
	successes, failures                                        int
	recoveries                                                 []time.Time
	stalled, recommended                                       bool
}
type Policy struct {
	mu           sync.Mutex
	profiles     []vpnmodel.Profile
	networks     map[string]bool
	paths        map[string]*candidate
	active       map[string]string
	reserveProbe map[string]time.Time
	jitter       func(time.Duration) time.Duration
}

func New(c vpnmodel.Config) *Policy {
	p := &Policy{profiles: append([]vpnmodel.Profile(nil), c.Profiles...), networks: map[string]bool{}, paths: map[string]*candidate{}, active: map[string]string{}, reserveProbe: map[string]time.Time{}, jitter: func(d time.Duration) time.Duration { return time.Duration(float64(d) * (0.8 + rand.Float64()*0.4)) }}
	sort.SliceStable(p.profiles, func(i, j int) bool { return p.profiles[i].Priority < p.profiles[j].Priority })
	return p
}
func (p *Policy) SetNetwork(name string, allowed, available bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if name == "wifi" || name == "cell" {
		p.networks[name] = allowed && available
	}
}
func (p *Policy) Observe(o Observation) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.paths[o.PathID]
	if c == nil || c.generation != o.Generation || o.At.Before(c.last) || (c.state != "dialing" && c.state != "ready") {
		return
	}
	c.last = o.At
	if o.Failed {
		p.failed(c, o.At)
		return
	}
	progress := o.AckedBytes > c.health.acked
	if o.Connected {
		c.state = "ready"
	}
	if progress || o.DataProbeOK {
		if c.healthy.IsZero() {
			c.healthy = o.At
		}
		c.successes++
	}
	if progress {
		if c.progressSince.IsZero() || (!c.lastProgress.IsZero() && o.At.Sub(c.lastProgress) > 5*time.Second) {
			c.progressSince = o.At
		}
		c.lastProgress = o.At
		if o.At.Sub(c.progressSince) >= 30*time.Second {
			c.failures = 0
		}
		if c.stalled {
			c.recoveries = append(c.recoveries, o.At)
			c.stalled = false
		}
	} else if !c.lastProgress.IsZero() && o.At.Sub(c.lastProgress) > 5*time.Second {
		c.progressSince = time.Time{}
	}
	c.health.Observe(o)
}
func (p *Policy) failed(c *candidate, now time.Time) {
	c.state = "failed"
	c.healthy = time.Time{}
	c.successes = 0
	c.progressSince = time.Time{}
	c.failures++
	d := time.Second * time.Duration(1<<min(c.failures-1, 5))
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	c.retry = now.Add(p.jitter(d))
	if p.active[c.profile.ExitID] == c.id {
		delete(p.active, c.profile.ExitID)
	}
}
func decision(c *candidate, action, reason string) Decision {
	return Decision{PathID: c.id, ProfileID: c.profile.ID, Network: c.network, Generation: c.generation, Action: action, Reason: reason}
}

// Next emits commands once. Callers report dial/probe results with the returned generation.
// Network permissions must already incorporate user consent and the shared LTE ledger.
func (p *Policy) Next(now time.Time) []Decision {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []Decision
	var ordered []*candidate
	for _, pr := range p.profiles {
		if pr.Mode == "disabled" {
			continue
		}
		for _, network := range []string{"wifi", "cell"} {
			id := pr.ID + "/" + network
			c := p.paths[id]
			if c == nil {
				c = &candidate{profile: pr, network: network, id: id}
				p.paths[id] = c
			}
			ordered = append(ordered, c)
			if (c.state == "ready" || c.state == "dialing") && (!p.networks[network] || c.health.Stalled(now) || c.state == "dialing" && now.Sub(c.started) >= 10*time.Second) {
				reason := "network unavailable"
				if c.health.Stalled(now) {
					reason = "data stalled"
					c.stalled = true
				} else if p.networks[network] {
					reason = "dial timeout"
				}
				out = append(out, decision(c, "close", reason))
				p.failed(c, now)
			}
			recent := c.recoveries[:0]
			for _, at := range c.recoveries {
				if now.Sub(at) <= 10*time.Minute {
					recent = append(recent, at)
				}
			}
			c.recoveries = recent
			if len(recent) >= 3 && !c.recommended {
				out = append(out, decision(c, "recommend_carousel", "three connection recoveries in ten minutes"))
				c.recommended = true
			}
		}
	}
	exits := map[string]bool{}
	for _, pr := range p.profiles {
		exits[pr.ExitID] = true
	}
	// Iterate exits in stable profile order, never map order.
	for _, pr := range p.profiles {
		exit := pr.ExitID
		if !exits[exit] {
			continue
		}
		delete(exits, exit)
		var best *candidate
		for _, mode := range []string{"auto", "reserve"} {
			for _, c := range ordered {
				if c.profile.ExitID == exit && c.profile.Mode == mode && c.state == "ready" && c.successes > 0 && p.networks[c.network] {
					best = c
					break
				}
			}
			if best != nil {
				break
			}
		}
		current := p.paths[p.active[exit]]
		if best != nil && (current == nil || current.id != best.id && best.successes >= 3 && now.Sub(best.healthy) >= 8*time.Second) {
			p.active[exit] = best.id
			out = append(out, decision(best, "promote", "usable data path"))
		}
		// Exhaust all permitted automatic candidates before using reserve, including other networks.
		autoPending := false
		for _, c := range ordered {
			if c.profile.ExitID == exit && c.profile.Mode == "auto" && p.networks[c.network] && (c.state == "ready" || c.state == "dialing" || c.state == "") {
				autoPending = true
			}
		}
		for _, mode := range []string{"auto", "reserve"} {
			if mode == "reserve" && autoPending {
				break
			}
			for _, c := range ordered {
				if c.profile.ExitID != exit || c.profile.Mode != mode || !p.networks[c.network] {
					continue
				}
				if c.state == "ready" || c.state == "dialing" {
					break
				}
				if now.Before(c.retry) {
					continue
				}
				c.generation++
				c.state = "dialing"
				c.started = now
				c.last = now
				c.health = Health{}
				out = append(out, decision(c, "dial", "candidate available"))
				if mode == "auto" {
					autoPending = true
				}
				break
			}
		}
	}
	for _, pr := range p.profiles {
		if pr.Mode != "reserve" || !pr.CheckReserve || now.Before(p.reserveProbe[pr.ID]) {
			continue
		}
		for _, network := range []string{"wifi", "cell"} {
			c := p.paths[pr.ID+"/"+network]
			if p.networks[network] {
				if c.state != "ready" && c.state != "dialing" {
					out = append(out, decision(c, "probe", "optional reserve availability check"))
				}
				p.reserveProbe[pr.ID] = now.Add(p.jitter(time.Minute))
				break
			}
		}
	}
	return out
}
