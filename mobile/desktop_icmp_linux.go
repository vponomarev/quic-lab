//go:build linux && !android

package mobile

import (
	"context"
	"errors"
	"sync"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"quiclab/internal/icmprelay"
)

// Intercept before gVisor: ICMP must never get a synthetic local Echo response.
// The original fdbased endpoint remains the only TUN reader/writer and FD owner.
type linuxICMPLink struct {
	stack.LinkEndpoint
	attachMu   sync.Mutex
	mu         sync.RWMutex
	dispatcher stack.NetworkDispatcher
	closed     bool
	once       sync.Once
	echo       *icmprelay.Relay
}

func newLinuxICMPLink(inner stack.LinkEndpoint, exchange func(context.Context, []byte) ([]byte, error), written func(int)) *linuxICMPLink {
	l := &linuxICMPLink{LinkEndpoint: inner}
	l.echo = icmprelay.NewPacket(exchange, func(reply []byte) error {
		l.mu.RLock()
		defer l.mu.RUnlock()
		if l.closed {
			return errors.New("ICMP link closed")
		}
		pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(reply)})
		defer pkt.DecRef()
		pkt.NetworkProtocolNumber = header.IPv4ProtocolNumber
		var packets stack.PacketBufferList
		packets.PushBack(pkt)
		n, err := inner.WritePackets(packets)
		if err != nil || n != 1 {
			return errors.New("ICMP link write failed")
		}
		if written != nil {
			written(len(reply) - 20)
		}
		return nil
	})
	return l
}
func (l *linuxICMPLink) Attach(d stack.NetworkDispatcher) {
	l.attachMu.Lock()
	defer l.attachMu.Unlock()
	l.mu.Lock()
	if l.closed && d != nil {
		l.mu.Unlock()
		return
	}
	l.dispatcher = d
	l.mu.Unlock()
	if d == nil {
		l.LinkEndpoint.Attach(nil)
	} else {
		l.LinkEndpoint.Attach(l)
	}
}
func (l *linuxICMPLink) DeliverNetworkPacket(protocol tcpip.NetworkProtocolNumber, pkt *stack.PacketBuffer) {
	if protocol == header.IPv4ProtocolNumber {
		// Avoid copying ordinary TCP/UDP payloads just to inspect the protocol.
		if h, ok := pkt.Data().PullUp(20); ok && h[0]>>4 == 4 && h[9] == 1 {
			view := pkt.ToView()
			l.echo.Handle(view.AsSlice())
			view.Release()
			return // including malformed/unsupported ICMP; never deliver to stack
		}
	}
	l.mu.RLock()
	d, closed := l.dispatcher, l.closed
	l.mu.RUnlock()
	if !closed && d != nil {
		d.DeliverNetworkPacket(protocol, pkt)
	}
}
func (l *linuxICMPLink) DeliverLinkPacket(protocol tcpip.NetworkProtocolNumber, pkt *stack.PacketBuffer) {
	l.mu.RLock()
	d, closed := l.dispatcher, l.closed
	l.mu.RUnlock()
	if !closed && d != nil {
		d.DeliverLinkPacket(protocol, pkt)
	}
}
func (l *linuxICMPLink) Close() {
	l.once.Do(func() {
		l.attachMu.Lock()
		defer l.attachMu.Unlock()
		l.mu.Lock()
		l.closed = true
		l.dispatcher = nil
		l.mu.Unlock()
		l.echo.Close()
		// Closing a duplicated TUN FD does not reliably wake an in-flight poll.
		// Stop and join the reader before closing/reusing its descriptor.
		l.LinkEndpoint.Attach(nil)
		l.LinkEndpoint.Close()
	})
}

// ICMP follows the current VPN regardless of the TCP/UDP application policy.
// Replacement cancels pending probes; none can escape via the direct gateway.
func (r *DesktopAppRouter) echoLinux(ctx context.Context, packet []byte) ([]byte, error) {
	r.mu.Lock()
	if r.closed || r.vpn == nil {
		r.mu.Unlock()
		return nil, errors.New("VPN unavailable")
	}
	p := r.vpn
	p.h.mu.Lock()
	if p.h.closed {
		p.h.mu.Unlock()
		r.mu.Unlock()
		return nil, errors.New("VPN unavailable")
	}
	p.h.wg.Add(1)
	p.h.mu.Unlock()
	r.mu.Unlock()
	defer p.h.wg.Done()
	probe, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(p.h.ctx, cancel)
	defer stop()
	if err := p.h.ctx.Err(); err != nil {
		return nil, err
	}
	r.icmpTX.Add(int64(len(packet) - int(packet[0]&15)*4))
	reply, err := p.h.g.echoDesktop(probe, packet)
	if probe.Err() != nil {
		return nil, probe.Err()
	}
	return reply, err
}
