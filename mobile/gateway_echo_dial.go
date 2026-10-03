package mobile

import (
	"context"
	"quiclab/internal/gateway"
	"sync"
)

// This gate is separate from the VPN transport lock and application stream opens.
// A canceled diagnostic never accumulates additional opener/cleanup workers.
type diagnosticDialGate struct {
	once sync.Once
	slot chan struct{}
}

type diagnosticOwnedStream struct {
	gateway.Stream
	closeOnce      sync.Once
	closeRequested chan struct{}
}

func (s *diagnosticOwnedStream) Close() error {
	s.closeOnce.Do(func() { close(s.closeRequested) })
	return nil
}

func (g *Gateway) dialExitEcho(ctx context.Context, kind, target string) (gateway.Stream, error) {
	return g.openExitEchoDiagnostic(ctx, func(ctx context.Context) (gateway.Stream, error) { return g.dialStream(ctx, kind, target) })
}

// The one worker owns both the request handshake and underlying FIN cleanup.
// smux Close has a fixed 30s FIN wait; it must not delay the diagnostic caller or
// change shared VPN deadlines. The gate stays held until that cleanup finishes.
func (g *Gateway) openExitEchoDiagnostic(ctx context.Context, open func(context.Context) (gateway.Stream, error)) (gateway.Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gate := &g.exitEchoDialGate
	gate.once.Do(func() { gate.slot = make(chan struct{}, 1) })
	select {
	case gate.slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-gate.slot
		return nil, err
	}
	type result struct {
		stream *diagnosticOwnedStream
		err    error
	}
	ready := make(chan result)
	go func() {
		defer func() { <-gate.slot }()
		stream, err := open(ctx)
		if err != nil {
			if stream != nil {
				stream.Close()
			}
			select {
			case ready <- result{err: err}:
			case <-ctx.Done():
			}
			return
		}
		owned := &diagnosticOwnedStream{Stream: stream, closeRequested: make(chan struct{})}
		select {
		case ready <- result{stream: owned}:
			select {
			case <-owned.closeRequested:
			case <-ctx.Done():
			}
		case <-ctx.Done():
		}
		stream.Close()
	}()
	select {
	case opened := <-ready:
		if err := ctx.Err(); err != nil {
			if opened.stream != nil {
				opened.stream.Close()
			}
			return nil, err
		}
		return opened.stream, opened.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
