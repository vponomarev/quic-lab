package echo

import (
	"context"
	"quiclab/internal/protocol"
	"testing"
	"time"
)

func TestTransitDoesNotBlockLocalEcho(t *testing.T) {
	release := make(chan struct{})
	r := newTransitReplies(context.Background(), []func(context.Context) error{func(ctx context.Context) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	defer r.close()
	replies := make(chan protocol.Frame, 2)
	write := func(f protocol.Frame) error { replies <- f; return nil }
	if !r.handle(protocol.Frame{Transit: true}, write) {
		t.Fatal("not handled")
	}
	if r.handle(protocol.Frame{Seq: 2}, write) {
		t.Fatal("local echo captured")
	}
	close(release)
	select {
	case f := <-replies:
		if !f.TransitOK || !f.TransitSupported {
			t.Fatal("missing probe result")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}
