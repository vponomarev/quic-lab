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
	policy                                                        *approuting.TUNCompiled
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
	return &DesktopAppRouter{root: NewGateway(sink), vpn: newMultiProfile(vpn), direct: newMultiProfile(direct), policy: policy, owner: owner, ctx: ctx, cancel: cancel, pending: make(chan struct{}, 64)}, nil
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
			forward(r.vpn.h)
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
		return r.vpn.h
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
	switch r.policy.Decide(a) {
	case approuting.Tunnel:
		return r.vpn.h
	case approuting.Direct:
		return r.direct.h
	default:
		r.blocked.Add(1)
		return nil
	}
}
func (r *DesktopAppRouter) Snapshot() approuting.Statistics {
	return approuting.Statistics{Enabled: true, DirectTxBytes: uint64(r.direct.h.tx.Load()), DirectRxBytes: uint64(r.direct.h.rx.Load()), Resolved: r.resolved.Load(), Unknown: r.unknown.Load(), Shared: r.shared.Load(), Blocked: r.blocked.Load(), Overloaded: r.overloaded.Load(), TimedOut: r.timedOut.Load(), DNS: r.dns.Load()}
}
func (r *DesktopAppRouter) emitTraffic() {
	h := r.vpn.h
	r.root.emit("traffic", map[string]any{"tx_bytes": h.tx.Load(), "rx_bytes": h.rx.Load(), "tcp_flows": h.tcpFlows.Load(), "udp_flows": h.udpFlows.Load()})
}
func (r *DesktopAppRouter) Close() {
	r.once.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.cancel()
		r.mu.Unlock()
		r.wg.Wait()
		r.direct.stop()
		r.vpn.stop()
		r.emitTraffic()
		r.root.Stop()
	})
}
