package mobile

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPathProbeCancelPreservesWorkingTunnel(t *testing.T) {
	g, _, opened, closed := pendingEchoGateway(t)
	p := g.NewPathProbe()
	done := make(chan error, 1)
	go func() { _, err := p.Measure(); done <- err }()
	waitEchoAddress(t, opened, "")
	p.Stop()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled probe succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("probe cancellation blocked")
	}
	waitEchoAddress(t, closed, "")
	if g.ctx.Err() != nil || g.mux.IsClosed() {
		t.Fatal("probe stopped the working tunnel")
	}
	if err := g.StartExitEcho("healthy:9000", 60000); err != nil {
		t.Fatal(err)
	}
	waitEchoAddress(t, opened, "healthy:9000")
}

func TestPathProbeOneShotAndCanceledBeforeStart(t *testing.T) {
	calls := 0
	p := newPathProbe(context.Background(), func(context.Context) error { calls++; return nil })
	if elapsed, err := p.Measure(); err != nil || elapsed < 1 {
		t.Fatalf("elapsed=%d err=%v", elapsed, err)
	}
	if _, err := p.Measure(); err == nil || calls != 1 {
		t.Fatal("probe ran more than once")
	}
	p = newPathProbe(context.Background(), func(context.Context) error { t.Fatal("canceled probe dialed"); return nil })
	p.Stop()
	if _, err := p.Measure(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestPathProbeFailureAndParentCancellation(t *testing.T) {
	failure := errors.New("unverified response")
	p := newPathProbe(context.Background(), func(context.Context) error { return failure })
	if _, err := p.Measure(); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.Background())
	p = newPathProbe(parent, func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	cancel()
	if _, err := p.Measure(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := NewGateway(nil).NewPathProbe().Measure(); err == nil {
		t.Fatal("disconnected gateway accepted")
	}
}

func TestPathProbeHasBoundedDeadline(t *testing.T) {
	p := newPathProbe(context.Background(), func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Fatal("unbounded path probe")
		}
		return nil
	})
	if _, err := p.Measure(); err != nil {
		t.Fatal(err)
	}
}
