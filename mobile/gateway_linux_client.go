//go:build linux && !android

package mobile

import (
	"context"
)

// StartLinuxContext is a bounded Go-only client entry. Android keeps its own API.
func (g *Gateway) StartLinuxContext(ctx context.Context, raw string, binder SocketBinder) error {
	return g.startDesktopContext(ctx, raw, binder)
}
