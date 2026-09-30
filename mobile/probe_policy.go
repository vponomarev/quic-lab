package mobile

import (
	"context"
	"sync"
	"time"
)

// A nil policy preserves Echo's original cadence. VPN opts in explicitly.
type probePolicy struct {
	mu         sync.Mutex
	configured bool
	enabled    bool
	interval   time.Duration
	changed    chan struct{}
}

func (p *probePolicy) set(enabled bool, interval time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if interval < time.Second {
		interval = time.Second
	}
	if interval > 5*time.Second {
		interval = 5 * time.Second
	}
	if p.configured && p.enabled == enabled && p.interval == interval {
		return
	}
	p.configured, p.enabled, p.interval = true, enabled, interval
	if p.changed != nil {
		close(p.changed)
	}
	p.changed = make(chan struct{})
}
func (p *probePolicy) snapshot(fallback time.Duration) (bool, time.Duration, <-chan struct{}) {
	if p == nil {
		return true, fallback, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.changed == nil {
		p.changed = make(chan struct{})
	}
	if !p.configured {
		return true, fallback, p.changed
	}
	if !p.enabled {
		return false, 5 * time.Second, p.changed
	}
	return true, p.interval, p.changed
}
func (p *probePolicy) enabledNow() bool { enabled, _, _ := p.snapshot(time.Second); return enabled }

// Policy changes reset the deadline instead of sending an extra packet.
func (p *probePolicy) wait(ctx context.Context, fallback time.Duration) bool {
	return p.waitSince(ctx, fallback, time.Now())
}
func (p *probePolicy) waitSince(ctx context.Context, fallback time.Duration, started time.Time) bool {
	for {
		_, delay, changed := p.snapshot(fallback)
		timer := time.NewTimer(max(0, delay-time.Since(started)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-changed:
			timer.Stop()
			started = time.Now()
		case <-timer.C:
			return ctx.Err() == nil
		}
	}
}
func (p *probePolicy) event(kind string, values map[string]any) (string, bool) {
	if p.enabledNow() {
		return kind, true
	}
	switch kind {
	case "echo":
		delete(values, "rtt_ms")
		delete(values, "gap_ms")
		return "health", true
	case "transit_echo", "transit_probe_failed":
		return kind, false
	}
	return kind, true
}

// SetRTT controls VPN diagnostics; rare health probes remain for recovery.
func (g *Gateway) SetRTT(enabled bool, intervalMS int) {
	g.probes.set(enabled, time.Duration(intervalMS)*time.Millisecond)
}
