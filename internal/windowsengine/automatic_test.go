package windowsengine

import (
	"context"
	"errors"
	"quiclab/internal/vpnclient"
	"sync"
	"testing"
	"time"
)

type autoFake struct {
	ctx           context.Context
	mu            sync.Mutex
	starts, stops int
	failStop      bool
	unhealthy     bool
}

func (f *autoFake) Start(ctx context.Context, _ vpnclient.Config) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	f.ctx = ctx
	return ctx.Err()
}
func (f *autoFake) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	if f.failStop {
		return errors.New("cleanup")
	}
	return nil
}
func (f *autoFake) Config() vpnclient.Config           { return New(valid(), nil, nil).Config() }
func (f *autoFake) RuntimeInfo() vpnclient.RuntimeInfo { return vpnclient.RuntimeInfo{Mode: "vpn"} }
func awaitAuto(t *testing.T, b *Automatic, state string) {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		if b.RuntimeInfo().NetworkState == state {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("wanted %s got %s", state, b.RuntimeInfo().NetworkState)
}
func TestAutomaticNetworkRecoveryAndStop(t *testing.T) {
	var mu sync.Mutex
	u := Uplink{Index: 5, Address: "192.168.5.2", Gateway: "192.168.5.1"}
	events := make(chan struct{}, 8)
	var engines []*autoFake
	s := valid()
	s.UplinkMode = "auto"
	b := NewAutomatic(s, func(context.Context, Settings) (Uplink, error) {
		mu.Lock()
		defer mu.Unlock()
		if u.Index == 0 {
			return u, errors.New("no network")
		}
		return u, nil
	}, func(Settings) ManagedEngine {
		f := &autoFake{}
		mu.Lock()
		engines = append(engines, f)
		mu.Unlock()
		return f
	}, func(context.Context) (<-chan struct{}, func(), error) { return events, func() {}, nil }, time.Hour)
	if e := b.Start(context.Background(), b.Config()); e != nil {
		t.Fatal(e)
	}
	awaitAuto(t, b, "ready")
	mu.Lock()
	first := engines[0]
	mu.Unlock()
	first.mu.Lock()
	err := first.ctx.Err()
	first.mu.Unlock()
	if err != nil {
		t.Fatal("startup return canceled live tunnel", err)
	}
	mu.Lock()
	u.Index = 0
	mu.Unlock()
	events <- struct{}{}
	awaitAuto(t, b, "waiting_network")
	mu.Lock()
	u.Index = 7
	u.Address = "10.1.2.3"
	mu.Unlock()
	events <- struct{}{}
	awaitAuto(t, b, "ready")
	if e := b.Stop(context.Background()); e != nil {
		t.Fatal(e)
	}
	events <- struct{}{}
	time.Sleep(25 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(engines) != 2 {
		t.Fatal("unexpected starts", len(engines))
	}
	for _, f := range engines {
		f.mu.Lock()
		if f.stops != 1 {
			t.Error("not cleaned", f.stops)
		}
		f.mu.Unlock()
	}
}

func TestAutomaticSameInterfaceGatewayChangeAndCleanupFence(t *testing.T) {
	var mu sync.Mutex
	u := Uplink{Index: 5, Address: "192.168.5.2", Gateway: "192.168.5.1"}
	events := make(chan struct{}, 8)
	var engines []*autoFake
	s := valid()
	s.UplinkMode = "auto"
	b := NewAutomatic(s, func(context.Context, Settings) (Uplink, error) { mu.Lock(); defer mu.Unlock(); return u, nil }, func(Settings) ManagedEngine {
		mu.Lock()
		defer mu.Unlock()
		f := &autoFake{}
		engines = append(engines, f)
		return f
	}, func(context.Context) (<-chan struct{}, func(), error) { return events, func() {}, nil }, time.Hour)
	if err := b.Start(context.Background(), b.Config()); err != nil {
		t.Fatal(err)
	}
	defer b.Stop(context.Background())
	awaitAuto(t, b, "ready")
	events <- struct{}{}
	time.Sleep(220 * time.Millisecond)
	mu.Lock()
	n := len(engines)
	first := engines[0]
	mu.Unlock()
	if n != 1 {
		t.Fatal("unchanged network restarted engine")
	}
	first.mu.Lock()
	first.failStop = true
	first.mu.Unlock()
	mu.Lock()
	u.Gateway = "192.168.5.254"
	mu.Unlock()
	events <- struct{}{}
	awaitAuto(t, b, "cleanup_required")
	if err := b.Stop(context.Background()); err == nil {
		t.Fatal("cleanup failure ignored")
	}
	mu.Lock()
	n = len(engines)
	mu.Unlock()
	if n != 1 {
		t.Fatal("started over unclean engine")
	}
	first.mu.Lock()
	first.failStop = false
	first.mu.Unlock()
	if err := b.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(context.Background(), b.Config()); err != nil {
		t.Fatal(err)
	}
	awaitAuto(t, b, "ready")
	mu.Lock()
	n = len(engines)
	mu.Unlock()
	if n != 2 {
		t.Fatal("did not recover after cleanup")
	}
}

type blockedAutoFake struct {
	autoFake
	entered chan struct{}
}

func (f *blockedAutoFake) Start(ctx context.Context, c vpnclient.Config) error {
	close(f.entered)
	<-ctx.Done()
	return ctx.Err()
}
func TestAutomaticStopDuringStart(t *testing.T) {
	s := valid()
	s.UplinkMode = "auto"
	f := &blockedAutoFake{entered: make(chan struct{})}
	b := NewAutomatic(s, func(context.Context, Settings) (Uplink, error) { return Uplink{Index: 5}, nil }, func(Settings) ManagedEngine { return f }, func(context.Context) (<-chan struct{}, func(), error) { return nil, func() {}, nil }, time.Hour)
	if err := b.Start(context.Background(), b.Config()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.entered:
	case <-time.After(time.Second):
		t.Fatal("not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := b.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if b.RuntimeInfo().NetworkState != "" {
		t.Fatal("still reconnecting")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stops != 1 {
		t.Fatal("cleanup not completed")
	}
}

func (f *autoFake) Healthy() bool { f.mu.Lock(); defer f.mu.Unlock(); return !f.unhealthy }
func TestAutomaticDeadTransportSameNetwork(t *testing.T) {
	var mu sync.Mutex
	var engines []*autoFake
	s := valid()
	s.UplinkMode = "auto"
	b := NewAutomatic(s, func(context.Context, Settings) (Uplink, error) {
		return Uplink{Index: 5, Address: "192.168.5.2", Gateway: "192.168.5.1"}, nil
	}, func(Settings) ManagedEngine {
		mu.Lock()
		defer mu.Unlock()
		f := &autoFake{}
		engines = append(engines, f)
		return f
	}, func(context.Context) (<-chan struct{}, func(), error) { return nil, func() {}, nil }, 20*time.Millisecond)
	if err := b.Start(context.Background(), b.Config()); err != nil {
		t.Fatal(err)
	}
	defer b.Stop(context.Background())
	awaitAuto(t, b, "ready")
	mu.Lock()
	first := engines[0]
	mu.Unlock()
	first.mu.Lock()
	first.unhealthy = true
	first.mu.Unlock()
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		mu.Lock()
		n := len(engines)
		mu.Unlock()
		if n == 2 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("dead transport retained on unchanged uplink")
}
