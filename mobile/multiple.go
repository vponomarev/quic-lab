//go:build linux

package mobile

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"quiclab/internal/gateway"
	"quiclab/internal/routing"
	"sync"
	"syscall"
)

// FlowOwner looks up the originating Android UID; -1 means unknown.
// local is the app socket, remote is its destination, both numeric IPv4 addresses.
type FlowOwner interface {
	Owner(protocol int64, local string, localPort int64, remote string, remotePort int64) int64
}

type multiProfile struct {
	h      *tunHandler
	cancel context.CancelFunc
}

// MultiRouter owns one TUN, never the independent transport lifecycles.
// Rules are immutable while running; unavailable profiles retain their rules.
type MultiRouter struct {
	mu         sync.Mutex
	root       *Gateway
	policy     *routing.Policy
	owner      FlowOwner
	profiles   map[string]*multiProfile
	dnsProfile string
	disabled   map[string]bool
	closed     bool
}

func NewMultiRouter(raw string, dnsProfile string, owner FlowOwner, directBinder SocketBinder, sink EventSink) (*MultiRouter, error) {
	p, e := routing.Parse(raw)
	if e != nil {
		return nil, e
	}
	found := false
	for _, r := range p.Rules {
		if r.ID == dnsProfile {
			found = true
		}
	}
	if !found {
		return nil, errors.New("choose a DNS profile")
	}
	if directBinder == nil {
		return nil, errors.New("direct sockets require a VPN bypass binder")
	}
	m := &MultiRouter{root: NewGateway(sink), policy: p, owner: owner, profiles: map[string]*multiProfile{}, dnsProfile: dnsProfile, disabled: map[string]bool{}}
	g := NewGateway(sink)
	g.direct = protectedDialer{directBinder}
	m.profiles["direct"] = newMultiProfile(g)
	return m, nil
}
func newMultiProfile(g *Gateway) *multiProfile {
	ctx, cancel := context.WithCancel(context.Background())
	p := &multiProfile{h: &tunHandler{g: g, ctx: ctx, slots: make(chan struct{}, gateway.MaxFlows)}, cancel: cancel}
	// A profile outlives HTTPS transport contexts, which are replaced on migration.
	// Explicit router detach/pause cancels flows; transport errors close old streams.
	return p
}
func (p *multiProfile) stop() {
	p.h.mu.Lock()
	p.h.closed = true
	p.h.mu.Unlock()
	p.cancel()
	p.h.wg.Wait()
}
func (m *MultiRouter) Attach(fd int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("router closed")
	}
	return m.root.attachRouter(fd, m)
}

// SetGateway replaces only this profile; callers own starting/stopping Gateway.
func (m *MultiRouter) SetGateway(id string, g *Gateway) error {
	if g == nil {
		return errors.New("nil gateway")
	}
	m.mu.Lock()
	if m.closed || m.disabled[id] {
		m.mu.Unlock()
		return errors.New("router or profile stopped")
	}
	found := false
	for _, r := range m.policy.Rules {
		if r.ID == id {
			found = true
		}
	}
	if !found {
		m.mu.Unlock()
		return errors.New("unknown profile")
	}
	old := m.profiles[id]
	m.profiles[id] = newMultiProfile(g)
	m.mu.Unlock()
	if old != nil {
		old.stop()
	}
	return nil
}

// SetProfileEnabled immediately blocks new flows and cancels existing ones.
// Waiting for workers happens outside the Android main thread.
func (m *MultiRouter) SetProfileEnabled(id string, enabled bool) {
	if id == "direct" {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.disabled[id] = !enabled
	var old *multiProfile
	if !enabled {
		old = m.profiles[id]
		delete(m.profiles, id)
		if old != nil {
			old.cancel()
		}
	}
	m.mu.Unlock()
	if old != nil {
		go old.stop()
	}
}

// RemoveGateway stops flows, but retains the rule to prevent fallback leaks.
func (m *MultiRouter) RemoveGateway(id string) { m.removeGateway(id, nil) }

// DetachGateway ignores stale shutdown callbacks from a previous connection.
func (m *MultiRouter) DetachGateway(id string, g *Gateway) {
	if g != nil {
		m.removeGateway(id, g)
	}
}
func (m *MultiRouter) removeGateway(id string, g *Gateway) {
	if id == "direct" {
		return
	}
	m.mu.Lock()
	old := m.profiles[id]
	if g != nil && (old == nil || old.h.g != g) {
		m.mu.Unlock()
		return
	}
	delete(m.profiles, id)
	m.mu.Unlock()
	if old != nil {
		old.stop()
	}
}
func (m *MultiRouter) selectFlow(protocol int64, src string, sp int64, dst string, dp int64) *tunHandler {
	ip, e := netip.ParseAddr(dst)
	if e != nil || !ip.Is4() {
		return nil
	}
	uid := int64(-1)
	if m.owner != nil {
		uid = m.owner.Owner(protocol, src, sp, dst, dp)
	}
	d := m.policy.Choose(ip, uid)
	// One explicit resolver path. Never infer app identity from system DNS traffic.
	if dp == 53 {
		d = routing.Decision{Profile: m.dnsProfile, Reason: "dns_profile"}
	}
	m.mu.Lock()
	p := m.profiles[d.Profile]
	closed := m.closed || m.disabled[d.Profile]
	m.mu.Unlock()
	ready := !closed && p != nil && p.h.ctx.Err() == nil
	if !ready || d.Profile == "" {
		m.root.emit("route_blocked", map[string]any{"profile_id": d.Profile, "reason": d.Reason, "destination": dst, "uid": uid})
		return nil
	}
	m.root.emit("route_selected", map[string]any{"profile_id": d.Profile, "reason": d.Reason, "destination": dst, "uid": uid})
	return p.h
}
func (m *MultiRouter) emitTraffic() {
	m.mu.Lock()
	copy := make(map[string]*multiProfile, len(m.profiles))
	for k, v := range m.profiles {
		copy[k] = v
	}
	m.mu.Unlock()
	for id, p := range copy {
		h := p.h
		m.root.emit("profile_traffic", map[string]any{"profile_id": id, "tx_bytes": h.tx.Load(), "rx_bytes": h.rx.Load(), "tcp_flows": h.tcpFlows.Load(), "udp_flows": h.udpFlows.Load(), "udp_rejected": h.udpRejected.Load()})
	}
}
func (m *MultiRouter) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	ps := m.profiles
	m.profiles = map[string]*multiProfile{}
	m.mu.Unlock()
	for _, p := range ps {
		p.stop()
	}
	m.root.Stop()
}

type protectedDialer struct{ binder SocketBinder }

func (d protectedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, e := net.SplitHostPort(address)
	if e != nil || net.ParseIP(host).To4() == nil {
		return nil, errors.New("numeric IPv4 required")
	}
	dial := net.Dialer{Control: func(_, _ string, c syscall.RawConn) error {
		var bindErr error
		e := c.Control(func(fd uintptr) { bindErr = d.binder.Bind(int64(fd)) })
		if e != nil {
			return e
		}
		return bindErr
	}}
	return dial.DialContext(ctx, network, address)
}
