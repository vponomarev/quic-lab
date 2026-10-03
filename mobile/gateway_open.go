package mobile

import (
	"context"
	"quiclab/internal/gateway"

	"github.com/xtaci/smux"
)

// smux has no context-aware OpenStream: a blocked SYN can wait for its own
// 30-second timeout. Keep at most one such opener per Gateway, including its
// eventual cleanup, so canceled callers return promptly without accumulating
// goroutines or changing deadlines on the shared VPN transport.
func (g *Gateway) openMuxStream(ctx context.Context, mux *smux.Session) (gateway.Stream, error) {
	g.mu.Lock()
	if g.muxOpenSlot == nil {
		g.muxOpenSlot = make(chan struct{}, 1)
	}
	slot := g.muxOpenSlot
	g.mu.Unlock()
	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-slot
		return nil, err
	}
	type result struct {
		stream *smux.Stream
		err    error
	}
	ready := make(chan result)
	go func() {
		defer func() { <-slot }()
		stream, err := mux.OpenStream()
		select {
		case ready <- result{stream, err}:
		case <-ctx.Done():
			if stream != nil {
				stream.Close()
			}
		}
	}()
	select {
	case opened := <-ready:
		if opened.err != nil {
			return nil, opened.err
		}
		return gateway.NewMStream(opened.stream), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
