//go:build windows

package mobile

import (
	"context"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// AttachWindowsLink takes ownership of a Windows packet endpoint; not exported in gomobile builds.
func (g *Gateway) AttachWindowsLink(link stack.LinkEndpoint) error { return g.attachLink(link, nil) }

// StartWindowsContext is a Go-only bounded engine entry; Android keeps Start.
func (g *Gateway) StartWindowsContext(ctx context.Context, configJSON string, binder SocketBinder) error {
	return g.startDesktopContext(ctx, configJSON, binder)
}

// EchoWindows uses the authenticated tunnel only; never fall back to direct IP.
func (g *Gateway) EchoWindows(ctx context.Context, packet []byte) ([]byte, error) {
	return g.echoDesktop(ctx, packet)
}
