//go:build linux

package mobile

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/adapter"
	"github.com/xjasonlyu/tun2socks/v2/core/device/fdbased"
	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"quiclab/internal/gateway"
)

// Both explicit shutdown and Stack.Wait close the link endpoint. FD.Close in
// tun2socks is not idempotent: a second close can hit a reused Android FD.
type ownedLinkEndpoint struct {
	stack.LinkEndpoint
	once sync.Once
}

func (e *ownedLinkEndpoint) Close() { e.once.Do(e.LinkEndpoint.Close) }

// Attach duplicates a borrowed Android TUN fd. The service retains its original.
func (g *Gateway) Attach(fd int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.tunStop != nil {
		return fmt.Errorf("TUN already attached")
	}
	dup, e := unix.Dup(fd)
	if e != nil {
		return e
	}
	unix.CloseOnExec(dup)
	dev, e := fdbased.Open(strconv.Itoa(dup), 1280, 0)
	if e != nil {
		unix.Close(dup)
		return e
	}
	link := &ownedLinkEndpoint{LinkEndpoint: dev}
	ctx, cancel := context.WithCancel(context.Background())
	h := &tunHandler{g: g, ctx: ctx, slots: make(chan struct{}, gateway.MaxFlows)}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				g.emit("traffic", map[string]any{"tx_bytes": h.tx.Load(), "rx_bytes": h.rx.Load()})
			}
		}
	}()
	s, e := core.CreateStack(&core.Config{LinkEndpoint: link, TransportHandler: h})
	if e != nil {
		cancel()
		link.Close()
		return e
	}
	g.tunStop = func() {
		h.mu.Lock()
		h.closed = true
		h.mu.Unlock()
		cancel()
		s.Close()
		link.Close()
		s.Wait()
		h.wg.Wait()
	}
	return nil
}

type tunHandler struct {
	tx, rx atomic.Int64
	mu     sync.Mutex
	closed bool
	g      *Gateway
	ctx    context.Context
	slots  chan struct{}
	wg     sync.WaitGroup
}

func (h *tunHandler) HandleTCP(c adapter.TCPConn) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		c.Close()
		return
	}
	select {
	case h.slots <- struct{}{}:
	default:
		h.mu.Unlock()
		c.Close()
		return
	}
	h.wg.Add(1)
	h.mu.Unlock()
	go func() {
		defer h.wg.Done()
		defer func() { <-h.slots }()
		defer c.Close()
		id := c.ID()
		dst := net.JoinHostPort(id.LocalAddress.String(), strconv.Itoa(int(id.LocalPort)))
		ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
		s, e := h.g.dialStream(ctx, "tcp", dst)
		cancel()
		if e != nil {
			h.g.emit("flow_failed", map[string]any{"destination": dst, "error": e.Error()})
			return
		}
		h.g.emit("flow_open", map[string]any{"destination": dst})
		up, down := gateway.Relay(h.ctx, trafficStream{Stream: s, tx: &h.tx, rx: &h.rx}, c)
		h.g.emit("flow_closed", map[string]any{"destination": dst, "up": up, "down": down})
	}()
}

// QUIC/HTTPS support DNS datagrams; AWG can carry arbitrary IPv4 UDP.
func (h *tunHandler) HandleUDP(c adapter.UDPConn) {
	id := c.ID()
	backend := h.g.datagramBackend()
	if backend == nil && id.LocalPort != 53 {
		c.Close()
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		c.Close()
		return
	}
	select {
	case h.slots <- struct{}{}:
	default:
		h.mu.Unlock()
		c.Close()
		return
	}
	h.wg.Add(1)
	h.mu.Unlock()
	go func() {
		defer h.wg.Done()
		defer func() { <-h.slots }()
		defer c.Close()
		if backend != nil {
			h.relayUDP(c, backend)
			return
		}
		ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
		s, e := gateway.Open(ctx, h.g.open, "dns", net.JoinHostPort(id.LocalAddress.String(), "53"))
		cancel()
		if e != nil {
			return
		}
		s = trafficStream{Stream: s, tx: &h.tx, rx: &h.rx}
		defer s.Close()
		stop := context.AfterFunc(h.ctx, func() { c.Close(); s.Close() })
		defer stop()
		b := make([]byte, 4096)
		for {
			c.SetDeadline(time.Now().Add(30 * time.Second))
			n, from, e := c.ReadFrom(b)
			if e != nil {
				return
			}
			s.SetDeadline(time.Now().Add(8 * time.Second))
			if gateway.WritePacket(s, b[:n]) != nil {
				return
			}
			reply, e := gateway.ReadPacket(s, 4096)
			if e != nil {
				return
			}
			if _, e = c.WriteTo(reply, from); e != nil {
				return
			}
		}
	}()
}

// A connected UDP flow uses independent read/write loops, preserving datagram boundaries.
func (h *tunHandler) relayUDP(c adapter.UDPConn, d flowDialer) {
	id := c.ID()
	dst := net.JoinHostPort(id.LocalAddress.String(), strconv.Itoa(int(id.LocalPort)))
	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	out, e := d.DialContext(ctx, "udp4", dst)
	cancel()
	if e != nil {
		return
	}
	defer out.Close()
	stop := context.AfterFunc(h.ctx, func() { out.Close(); c.Close() })
	defer stop()
	b := make([]byte, 65535)
	c.SetDeadline(time.Now().Add(60 * time.Second))
	n, from, e := c.ReadFrom(b)
	if e != nil {
		return
	}
	out.SetDeadline(time.Now().Add(60 * time.Second))
	if _, e = out.Write(b[:n]); e != nil {
		return
	}
	h.tx.Add(int64(n))
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer c.Close()
		reply := make([]byte, 65535)
		for {
			n, e := out.Read(reply)
			if e != nil {
				return
			}
			if _, e = c.WriteTo(reply[:n], from); e != nil {
				return
			}
			h.rx.Add(int64(n))
			c.SetDeadline(time.Now().Add(60 * time.Second))
			out.SetDeadline(time.Now().Add(60 * time.Second))
		}
	}()
	defer func() { out.Close(); <-done }()
	for {
		n, _, e = c.ReadFrom(b)
		if e != nil {
			return
		}
		if _, e = out.Write(b[:n]); e != nil {
			return
		}
		h.tx.Add(int64(n))
		c.SetDeadline(time.Now().Add(60 * time.Second))
		out.SetDeadline(time.Now().Add(60 * time.Second))
	}
}
