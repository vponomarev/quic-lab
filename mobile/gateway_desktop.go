//go:build (linux && !android) || windows

package mobile

import (
	"context"
	"encoding/json"
	"errors"
)

// Desktop adapters explicitly support private TLS roots; Android keeps its API.
func (g *Gateway) startDesktopContext(ctx context.Context, raw string, binder SocketBinder) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	var c gatewayConfig
	if json.Unmarshal([]byte(raw), &c) != nil || (c.Transport != "quic" && c.Transport != "https" && c.Transport != "awg" && c.Transport != "vless") || c.MaxAvailability || c.hasUnderlay() {
		return errors.New("unsupported desktop transport")
	}
	if c.Transport == "vless" {
		g.mu.Lock()
		defer g.mu.Unlock()
		if e := ctx.Err(); e != nil {
			return e
		}
		if g.cancel != nil {
			return errors.New("gateway already running")
		}
		g.cfg = c
		return g.startVLESSLockedWithRoots(binder, []byte(c.CA))
	}
	return g.startWithContext(ctx, raw, binder)
}
