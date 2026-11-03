package mobile

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"
)

// PathProbe measures a common verified HTTPS request through a VPN gateway.
type PathProbe struct {
	ctx     context.Context
	cancel  context.CancelFunc
	measure func(context.Context) error
	started atomic.Bool
}

// NewPathProbe does not attach a TUN, change diagnostics, or expose the exit IP.
// Its lifetime belongs to the current gateway session; Stop cancels only this request.
func (g *Gateway) NewPathProbe() *PathProbe {
	g.mu.Lock()
	parent := g.ctx
	running := g.cancel != nil
	g.mu.Unlock()
	if parent == nil || !running {
		var cancel context.CancelFunc
		parent, cancel = context.WithCancel(context.Background())
		cancel()
	}
	return newPathProbe(parent, func(ctx context.Context) error {
		_, err := fetchExitIP(ctx, func(call context.Context) (net.Conn, error) {
			stream, err := g.dialStream(call, "exit-ip", "")
			if err != nil {
				return nil, err
			}
			return diagnosticConn{stream}, nil
		})
		return err
	})
}

func newPathProbe(parent context.Context, measure func(context.Context) error) *PathProbe {
	ctx, cancel := context.WithCancel(parent)
	return &PathProbe{ctx: ctx, cancel: cancel, measure: measure}
}

// Measure performs one verified, bounded HTTPS request over this tunnel only.
func (p *PathProbe) Measure() (int64, error) {
	if !p.started.CompareAndSwap(false, true) {
		return 0, errors.New("path probe already used")
	}
	defer p.cancel()
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()
	started := time.Now()
	if err := p.measure(ctx); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return max(1, time.Since(started).Milliseconds()), nil
}

func (p *PathProbe) Stop() { p.cancel() }
