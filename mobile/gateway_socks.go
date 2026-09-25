package mobile

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"time"

	"quiclab/internal/gateway"
)

// ListenSOCKS exposes optional IPv4 SOCKS5 CONNECT on loopback for desktop tests.
// Use curl --socks5, not --socks5-hostname: DNS belongs to the desktop client.
func (g *Gateway) ListenSOCKS(address string) (string, error) {
	host, _, e := net.SplitHostPort(address)
	if e != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return "", errors.New("SOCKS listener must be loopback")
	}
	ln, e := net.Listen("tcp4", address)
	if e != nil {
		return "", e
	}
	g.mu.Lock()
	if g.cancel == nil {
		g.mu.Unlock()
		ln.Close()
		return "", errors.New("start gateway first")
	}
	ctx := g.ctx
	g.mu.Unlock()
	context.AfterFunc(ctx, func() { ln.Close() })
	go func() {
		slots := make(chan struct{}, gateway.MaxFlows)
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			select {
			case slots <- struct{}{}:
				go func() { defer func() { <-slots }(); g.socks(ctx, c) }()
			default:
				c.Close()
			}
		}
	}()
	return ln.Addr().String(), nil
}
func (g *Gateway) socks(ctx context.Context, c net.Conn) {
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	var h [2]byte
	if _, e := io.ReadFull(c, h[:]); e != nil || h[0] != 5 {
		return
	}
	methods := make([]byte, int(h[1]))
	if _, e := io.ReadFull(c, methods); e != nil {
		return
	}
	allowed := false
	for _, m := range methods {
		if m == 0 {
			allowed = true
		}
	}
	if !allowed {
		c.Write([]byte{5, 255})
		return
	}
	c.Write([]byte{5, 0})
	var r [10]byte
	if _, e := io.ReadFull(c, r[:4]); e != nil {
		return
	}
	if r[0] != 5 || r[1] != 1 || r[2] != 0 || r[3] != 1 {
		c.Write([]byte{5, 8, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	if _, e := io.ReadFull(c, r[4:]); e != nil {
		return
	}
	target := net.JoinHostPort(net.IP(r[4:8]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(r[8:]))))
	s, e := g.dialStream(ctx, "tcp", target)
	if e != nil {
		c.Write([]byte{5, 2, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	if _, e = c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); e != nil {
		s.Close()
		return
	}
	c.SetDeadline(time.Time{})
	gateway.Relay(ctx, s, c)
}
