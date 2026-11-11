//go:build linux && !android

package linuxclient

import (
	"quiclab/mobile"
	"sync"
)

// Retiring an engine fences off callbacks already queued by its reader. A late
// reply from the old transport must not make its replacement appear healthy.
type gatewayEvents struct {
	mu      sync.Mutex
	sink    mobile.EventSink
	retired bool
}

func (e *gatewayEvents) OnEvent(raw string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.retired && e.sink != nil {
		e.sink.OnEvent(raw)
	}
}

func (e *gatewayEvents) retire() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.retired = true
}

// The caller owns Client.op. Event callbacks never acquire Client.mu.
func (c *Client) newGateway() *mobile.Gateway {
	c.gatewayEvents = &gatewayEvents{sink: c.Sink}
	g := mobile.NewGateway(c.gatewayEvents)
	g.SetRTT(true, 1000) // VLESS enforces its own 5 s HTTPS minimum.
	return g
}
