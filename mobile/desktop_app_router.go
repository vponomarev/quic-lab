//go:build (linux && !android) || windows

package mobile

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"

	"github.com/xjasonlyu/tun2socks/v2/core/adapter"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"quiclab/internal/approuting"
)

// DesktopAppRouter owns TUN/flow lifetimes, not the supplied VPN transport.
// Selection runs once per flow off the packet callback. Each exit retains its
// own bounded TCP/UDP handlers and UDP idle deadlines. No failure fallback.
type DesktopAppRouter struct {
	root                                                          *Gateway
	vpn, direct                                                   *multiProfile
	policy                                                        atomic.Pointer[approuting.TUNCompiled]
	change                                                        sync.Mutex
	retiring                                                      *multiProfile
	retiredTx, retiredRx, retiredTCP, retiredUDP                  int64
	owner                                                         approuting.OwnerResolver
	ctx                                                           context.Context
	cancel                                                        context.CancelFunc
	mu                                                            sync.Mutex
	closed                                                        bool
	pending                                                       chan struct{}
	wg                                                            sync.WaitGroup
	once                                                          sync.Once
	resolved, unknown, shared, blocked, overloaded, timedOut, dns atomic.Uint64
}

func NewDesktopAppRouter(policy *approuting.TUNCompiled, owner approuting.OwnerResolver, binder SocketBinder, vpn *Gateway, sink EventSink) (*DesktopAppRouter, error) {
	if policy == nil || owner == nil || binder == nil || vpn == nil {
		return nil, errors.New("application router requires policy, resolver, protected direct sockets and VPN")
	}
	ctx, cancel := context.WithCancel(context.Background())
	direct := NewGateway(nil)
	direct.direct = protectedDialer{binder}
	r := &DesktopAppRouter{root: NewGateway(sink), vpn: newMultiProfile(vpn), direct: newMultiProfile(direct), owner: owner, ctx: ctx, cancel: cancel, pending: make(chan struct{}, 64)}
	r.policy.Store(policy)
	return r, nil
}

// UpdatePolicy affects new flow selections. Existing handlers retain their exit.
func (r *DesktopAppRouter) UpdatePolicy(p *approuting.TUNCompiled) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || p == nil {
		return errors.New("router closed or invalid policy")
	}
	r.policy.Store(p)
	return nil
}

// ReplaceVPN retires only VPN handlers. Nil blocks tunnel/DNS flows. Callers
// own gateway lifetime; direct handlers and TUN survive replacement.
func (r *DesktopAppRouter) ReplaceVPN(g *Gateway) error {
	r.change.Lock()
	defer r.change.Unlock()
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errors.New("router closed")
	}
	old := r.vpn
	r.vpn = nil
	r.retiring = old
	if g != nil {
		r.vpn = newMultiProfile(g)
	}
	r.mu.Unlock()
	if old != nil {
		old.stop()
	}
	r.mu.Lock()
	if old != nil {
		r.retiredTx += old.h.tx.Load()
		r.retiredRx += old.h.rx.Load()
		r.retiredTCP += old.h.tcpFlows.Load()
		r.retiredUDP += old.h.udpFlows.Load()
	}
	r.retiring = nil
	r.mu.Unlock()
	return nil
}
func (r *DesktopAppRouter) vpnHandler() *tunHandler {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.vpn == nil {
		return nil
	}
	return r.vpn.h
}

func (r *DesktopAppRouter) Attach(link stack.LinkEndpoint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		if link != nil {
			link.Close()
		}
		return errors.New("application router closed")
	}
	return r.root.attachLink(link, r)
}
func desktopFlow(protocol uint8, id *stack.TransportEndpointID) approuting.FlowKey {
	src, _ := netip.ParseAddr(id.RemoteAddress.String())
	dst, _ := netip.ParseAddr(id.LocalAddress.String())
	return approuting.FlowKey{Protocol: protocol, Source: netip.AddrPortFrom(src, id.RemotePort), Destination: netip.AddrPortFrom(dst, id.LocalPort)}
}
func (r *DesktopAppRouter) HandleTCP(c adapter.TCPConn) {
	r.dispatch(desktopFlow(6, c.ID()), func(h *tunHandler) {
		if h == nil {
			c.Close()
		} else {
			h.HandleTCP(c)
		}
	})
}
func (r *DesktopAppRouter) routeTCP(c adapter.TCPConn) { r.HandleTCP(c) }
func (r *DesktopAppRouter) routeUDP(c adapter.UDPConn) { r.HandleUDP(c) }
func (r *DesktopAppRouter) HandleUDP(c adapter.UDPConn) {
	r.dispatch(desktopFlow(17, c.ID()), func(h *tunHandler) {
		if h == nil {
			c.Close()
		} else {
			h.HandleUDP(c)
		}
	})
}

func (r *DesktopAppRouter) dispatch(f approuting.FlowKey, forward func(*tunHandler)) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		forward(nil)
		return
	}
	r.wg.Add(1)
	select {
	case r.pending <- struct{}{}:
		r.mu.Unlock()
		go func() { defer r.wg.Done(); defer func() { <-r.pending }(); forward(r.selectFlow(f)) }()
	default:
		r.mu.Unlock()
		defer r.wg.Done()
		// Saturation is unknown ownership, not permission to silently change exit.
		if f.Destination.Port() == 53 {
			r.dns.Add(1)
			forward(r.vpnHandler())
			return
		}
		forward(r.choose(approuting.Attribution{Quality: approuting.Unknown, Reason: "overloaded"}))
	}
}
func (r *DesktopAppRouter) selectFlow(f approuting.FlowKey) *tunHandler {
	if r.ctx.Err() != nil {
		return nil
	}
	if !f.Source.Addr().Is4() || !f.Destination.Addr().Is4() {
		r.blocked.Add(1)
		return nil
	}
	// DNS is deliberately global, including direct applications. Windows DNS
	// Client may own the socket on behalf of an unrelated originating process.
	if f.Destination.Port() == 53 {
		r.dns.Add(1)
		return r.vpnHandler()
	}
	if !r.policy.Load().Enabled() {
		return r.vpnHandler()
	}
	a := r.owner.Resolve(r.ctx, f)
	if r.ctx.Err() != nil {
		return nil
	}
	return r.choose(a)
}
func (r *DesktopAppRouter) choose(a approuting.Attribution) *tunHandler {
	switch a.Quality {
	case approuting.Resolved:
		r.resolved.Add(1)
	case approuting.Shared:
		r.shared.Add(1)
	default:
		r.unknown.Add(1)
	}
	if a.Reason == "overloaded" {
		r.overloaded.Add(1)
	}
	if a.Reason == "timeout" {
		r.timedOut.Add(1)
	}
	switch r.policy.Load().Decide(a) {
	case approuting.Tunnel:
		return r.vpnHandler()
	case approuting.Direct:
		return r.direct.h
	default:
		r.blocked.Add(1)
		return nil
	}
}
func (r *DesktopAppRouter) Snapshot() approuting.Statistics {
	return approuting.Statistics{Enabled: r.policy.Load().Enabled(), DirectTxBytes: uint64(r.direct.h.tx.Load()), DirectRxBytes: uint64(r.direct.h.rx.Load()), Resolved: r.resolved.Load(), Unknown: r.unknown.Load(), Shared: r.shared.Load(), Blocked: r.blocked.Load(), Overloaded: r.overloaded.Load(), TimedOut: r.timedOut.Load(), DNS: r.dns.Load()}
}
func (r *DesktopAppRouter) emitTraffic() {
	tx, rx, tcp, udp := r.vpnTotals()
	r.root.emit("traffic", map[string]any{"tx_bytes": tx, "rx_bytes": rx, "tcp_flows": tcp, "udp_flows": udp})
}
func (r *DesktopAppRouter) vpnTotals() (tx, rx, tcp, udp int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tx, rx, tcp, udp = r.retiredTx, r.retiredRx, r.retiredTCP, r.retiredUDP
	for _, p := range []*multiProfile{r.vpn, r.retiring} {
		if p != nil {
			tx += p.h.tx.Load()
			rx += p.h.rx.Load()
			tcp += p.h.tcpFlows.Load()
			udp += p.h.udpFlows.Load()
		}
	}
	return
}
func (r *DesktopAppRouter) Close() {
	r.once.Do(func() {
		r.change.Lock()
		defer r.change.Unlock()
		r.mu.Lock()
		r.closed = true
		r.cancel()
		r.mu.Unlock()
		r.wg.Wait()
		r.direct.stop()
		if r.vpn != nil {
			r.vpn.stop()
		}
		r.emitTraffic()
		r.root.Stop()
	})
}
