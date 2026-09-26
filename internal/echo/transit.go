package echo

import (
	"context"
	"quiclab/internal/protocol"
	"sync"
	"time"
)

var probeSlots = make(chan struct{}, 16)

type transitReplies struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	last   time.Time
	probe  func(context.Context) error
}

func newTransitReplies(parent context.Context, probes []func(context.Context) error) *transitReplies {
	ctx, cancel := context.WithCancel(parent)
	r := &transitReplies{ctx: ctx, cancel: cancel}
	if len(probes) > 0 {
		r.probe = probes[0]
	}
	return r
}
func (r *transitReplies) close() { r.cancel(); r.wg.Wait() }

// Called serially by the reader. Slow probes never block normal echo replies.
func (r *transitReplies) handle(f protocol.Frame, write func(protocol.Frame) error) bool {
	if !f.Transit {
		return false
	}
	f.TransitOK = false
	f.TransitSupported = r.probe != nil
	if r.probe == nil || time.Since(r.last) < time.Second {
		write(f)
		return true
	}
	r.last = time.Now()
	select {
	case probeSlots <- struct{}{}:
	default:
		write(f)
		return true
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() { <-probeSlots }()
		ctx, cancel := context.WithTimeout(r.ctx, 2*time.Second)
		defer cancel()
		f.TransitOK = r.probe(ctx) == nil
		if r.ctx.Err() == nil {
			write(f)
		}
	}()
	return true
}
