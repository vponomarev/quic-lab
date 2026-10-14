//go:build windows

package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"net/netip"
	"quiclab/internal/gateway"
	"quiclab/internal/icmprelay"
)

// AttachWindowsLink takes ownership of a Windows packet endpoint; not exported in gomobile builds.
func (g *Gateway) AttachWindowsLink(link stack.LinkEndpoint) error { return g.attachLink(link, nil) }

// StartWindowsContext is a Go-only bounded engine entry; Android keeps Start.
func (g *Gateway) StartWindowsContext(ctx context.Context, configJSON string, binder SocketBinder) error {
	var c gatewayConfig
	if json.Unmarshal([]byte(configJSON), &c) != nil || (c.Transport != "quic" && c.Transport != "https") || c.MaxAvailability {
		return errors.New("unsupported Windows MVP transport")
	}
	return g.startWithContext(ctx, configJSON, binder)
}

// EchoWindows uses the authenticated tunnel only; never fall back to direct IP.
func (g *Gateway) EchoWindows(ctx context.Context, address netip.Addr, message []byte) ([]byte, error) {
	st, e := g.dialStream(ctx, "icmp-echo-v1", address.String())
	if e != nil {
		return nil, e
	}
	defer st.Close()
	stop := context.AfterFunc(ctx, func() { st.Close() })
	defer stop()
	if d, ok := ctx.Deadline(); ok {
		st.SetDeadline(d)
	}
	if e = gateway.WritePacket(st, message); e != nil {
		return nil, e
	}
	return gateway.ReadPacket(st, icmprelay.MaxMessage)
}
