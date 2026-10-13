package vpnclient

import (
	"context"
	"encoding/json"
	"errors"
	"quiclab/internal/vpnmodel"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeBackend struct {
	starts, stops atomic.Int32
	entered       chan struct{}
	block         bool
	fail          bool
	cleanupFail   atomic.Bool
}

func (b *fakeBackend) Start(ctx context.Context, c Config) error {
	b.starts.Add(1)
	if b.entered != nil {
		close(b.entered)
	}
	if b.block {
		<-ctx.Done()
		return ctx.Err()
	}
	if b.fail {
		return errors.New("private-key=SECRET")
	}
	return nil
}
func (b *fakeBackend) Stop(context.Context) error {
	b.stops.Add(1)
	if b.cleanupFail.Load() {
		return errors.New("secret cleanup error")
	}
	return nil
}
func cfg() Config {
	return Config{Revision: 7, Profiles: vpnmodel.Config{Version: 1, Exits: []vpnmodel.Exit{{ID: "a", Name: "A", Kind: "standalone"}}, Profiles: []vpnmodel.Profile{{ID: "p", ExitID: "a", Transport: "quic", Mode: "auto", Endpoint: "example.org:443", PoolSize: 1}}}}
}
func TestControllerLifecycleAndUIIndependence(t *testing.T) {
	b := &fakeBackend{}
	c := NewController(b)
	ctx, cancel := context.WithCancel(context.Background())
	if err := c.Start(ctx, cfg()); err != nil {
		t.Fatal(err)
	}
	cancel()
	if c.Snapshot().State != Running {
		t.Fatal(c.Snapshot())
	}
	if err := c.Start(context.Background(), cfg()); err != nil {
		t.Fatal(err)
	}
	if b.starts.Load() != 1 {
		t.Fatal("duplicate start")
	}
	next := cfg()
	next.Revision++
	if c.Start(context.Background(), next) == nil {
		t.Fatal("running config replaced")
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Stop(context.Background())
	if b.stops.Load() != 1 || c.Snapshot().State != Stopped {
		t.Fatal(c.Snapshot(), b.stops.Load())
	}
}
func TestStopCancelsStart(t *testing.T) {
	b := &fakeBackend{block: true, entered: make(chan struct{})}
	c := NewController(b)
	done := make(chan error, 1)
	go func() { done <- c.Start(context.Background(), cfg()) }()
	<-b.entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if <-done == nil {
		t.Fatal("cancelled start succeeded")
	}
	if b.stops.Load() != 1 {
		t.Fatal("cleanup count", b.stops.Load())
	}
	if c.Snapshot().State != Stopped {
		t.Fatal(c.Snapshot())
	}
}
func TestStartFailureIsRedactedAndCleanupRetryRequired(t *testing.T) {
	b := &fakeBackend{fail: true}
	b.cleanupFail.Store(true)
	c := NewController(b)
	err := c.Start(context.Background(), cfg())
	if err == nil {
		t.Fatal("start succeeded")
	}
	raw, _ := json.Marshal(c.Snapshot())
	if strings.Contains(string(raw)+err.Error(), "SECRET") || strings.Contains(string(raw), "private-key") {
		t.Fatal("secret leaked")
	}
	if c.Start(context.Background(), cfg()) == nil || b.starts.Load() != 1 {
		t.Fatal("dirty state allowed start")
	}
	b.cleanupFail.Store(false)
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().State != Stopped {
		t.Fatal(c.Snapshot())
	}
}
func TestCancelledStartReleasesResources(t *testing.T) {
	b := &fakeBackend{block: true, entered: make(chan struct{})}
	c := NewController(b)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Start(ctx, cfg()) }()
	<-b.entered
	cancel()
	if <-done == nil {
		t.Fatal("cancelled start succeeded")
	}
	if b.stops.Load() != 1 {
		t.Fatal("resource leaked")
	}
}

func TestSameRevisionCannotReplaceConfig(t *testing.T) {
	b := &fakeBackend{}
	c := NewController(b)
	c.Start(context.Background(), cfg())
	defer c.Stop(context.Background())
	next := cfg()
	next.Profiles.Profiles[0].Endpoint = "different.example:443"
	if c.Start(context.Background(), next) == nil {
		t.Fatal("same revision silently accepted different config")
	}
}
func TestCancelledStopDoesNotCancelRunningBackend(t *testing.T) {
	b := &contextBackend{}
	c := NewController(b)
	c.Start(context.Background(), cfg())
	defer c.Stop(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c.Stop(ctx) == nil {
		t.Fatal("cancelled stop accepted")
	}
	if b.ctx.Err() != nil {
		t.Fatal("cancelled request stopped running backend")
	}
}

type contextBackend struct{ ctx context.Context }

func (b *contextBackend) Start(ctx context.Context, _ Config) error { b.ctx = ctx; return nil }
func (b *contextBackend) Stop(context.Context) error                { return nil }

func TestConcurrentStartAndStop(t *testing.T) {
	b := &fakeBackend{}
	c := NewController(b)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				c.Start(context.Background(), cfg())
			} else {
				c.Stop(context.Background())
			}
			_ = c.Snapshot()
		}(i)
	}
	wg.Wait()
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.starts.Load() != b.stops.Load() || c.Snapshot().State != Stopped {
		t.Fatal("leaked run", b.starts.Load(), b.stops.Load(), c.Snapshot())
	}
}

func TestPendingStopInhibitsNewStart(t *testing.T) {
	b := &fakeBackend{}
	c := NewController(b)
	c.gate <- struct{}{}
	stop := make(chan error, 1)
	go func() { stop <- c.Stop(context.Background()) }()
	until := time.Now().Add(time.Second)
	for c.Snapshot().State != Stopping && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	if c.Snapshot().State != Stopping {
		<-c.gate
		<-stop
		t.Fatal("Stop did not publish intent before gate wait")
	}
	started := make(chan error, 1)
	go func() { started <- c.Start(context.Background(), cfg()) }()
	select {
	case err := <-started:
		if err == nil {
			t.Fatal("start accepted behind Stop")
		}
	case <-time.After(time.Second):
		<-c.gate
		<-stop
		c.Stop(context.Background())
		t.Fatal("Start did not honor pending Stop")
	}
	<-c.gate
	if err := <-stop; err != nil {
		t.Fatal(err)
	}
	if b.starts.Load() != 0 {
		t.Fatal("backend started during Stop")
	}
}
