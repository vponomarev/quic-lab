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
	"quiclab/internal/gateway"
)

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
	s, e := core.CreateStack(&core.Config{LinkEndpoint: dev, TransportHandler: h})
	if e != nil {
		cancel()
		dev.Close()
		return e
	}
	g.tunStop = func() {
		h.mu.Lock()
		h.closed = true
		h.mu.Unlock()
		cancel()
		s.Close()
		dev.Close()
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
		s, e := gateway.Open(ctx, h.g.open, "tcp", dst)
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

// Initial VPN supports DNS over UDP only; arbitrary UDP is rejected, never leaked.
func (h *tunHandler) HandleUDP(c adapter.UDPConn) {
	id := c.ID()
	if id.LocalPort != 53 {
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
