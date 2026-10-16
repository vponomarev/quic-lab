//go:build linux && !android

package mobile

import (
	"context"
	"encoding/json"
	"errors"
)

// StartLinuxContext is a bounded Go-only client entry. Android keeps its own API.
func (g *Gateway) StartLinuxContext(ctx context.Context, raw string, binder SocketBinder) error {
	var c gatewayConfig
	if json.Unmarshal([]byte(raw), &c) != nil || (c.Transport != "quic" && c.Transport != "https") || c.MaxAvailability {
		return errors.New("unsupported Linux client transport")
	}
	return g.startWithContext(ctx, raw, binder)
}
