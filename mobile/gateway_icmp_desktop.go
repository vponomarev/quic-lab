//go:build (linux && !android) || windows

package mobile

import (
	"context"
	"errors"
	"quiclab/internal/gateway"
)

// echoDesktop uses only the authenticated VPN; unsupported transports fail closed.
func (g *Gateway) echoDesktop(ctx context.Context, packet []byte) ([]byte, error) {
	g.mu.Lock()
	kind, awg := g.cfg.Transport, g.awg
	g.mu.Unlock()
	if kind == "awg" && awg != nil {
		return awg.EchoPacket(ctx, packet)
	}
	if kind != "quic" && kind != "https" {
		return nil, errors.New("ICMP forwarding unavailable for this transport")
	}
	return gateway.ExchangeICMP(ctx, g.dialStream, packet)
}
