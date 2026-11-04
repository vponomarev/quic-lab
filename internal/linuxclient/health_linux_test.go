//go:build linux && !android

package linuxclient

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestClientReadinessAndStopOwnHealth(t *testing.T) {
	c := &Client{}
	if c.Readiness().State != "stopped" {
		t.Fatal(c.Readiness())
	}
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	c.dev = f
	if c.Readiness().State != "engine_ready" {
		t.Fatal(c.Readiness())
	}
	started := make(chan struct{})
	h := newHealthMonitor(context.Background(), func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, time.Hour, time.Hour, false)
	c.health = h
	<-started
	if c.Readiness().State != "checking" {
		t.Fatal(c.Readiness())
	}
	done := make(chan struct{})
	go func() { c.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel probe")
	}
	select {
	case <-h.done:
	default:
		t.Fatal("health worker survived Stop")
	}
	if c.Readiness().State != "stopped" {
		t.Fatal(c.Readiness())
	}
}
