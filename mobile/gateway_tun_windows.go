//go:build windows

package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
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
