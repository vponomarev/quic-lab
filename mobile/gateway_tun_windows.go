//go:build windows

package mobile

import (
	"context"
	"errors"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"quiclab/internal/gateway"
)

// AttachWindowsLink takes ownership of a Windows packet endpoint; not exported in gomobile builds.
func (g *Gateway) AttachWindowsLink(link stack.LinkEndpoint) error { return g.attachLink(link, nil) }

// StartWindowsContext is a Go-only bounded engine entry; Android keeps Start.
func (g *Gateway) StartWindowsContext(ctx context.Context, configJSON string, binder SocketBinder) error {
	return g.startDesktopContext(ctx, configJSON, binder)
}

// EchoWindows uses the authenticated tunnel only; never fall back to direct IP.
func (g *Gateway) EchoWindows(ctx context.Context, packet []byte) ([]byte, error) {
	g.mu.Lock()
	kind := g.cfg.Transport
	awg := g.awg
	g.mu.Unlock()
	if kind == "awg" && awg != nil {
		return awg.EchoPacket(ctx, packet)
	}
	if kind != "quic" && kind != "https" {
		return nil, errors.New("ICMP forwarding unavailable for this transport")
	}
	return gateway.ExchangeICMP(ctx, g.dialStream, packet)
}
