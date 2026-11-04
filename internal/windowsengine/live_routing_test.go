package windowsengine

import (
	"context"
	"quiclab/internal/approuting"
	"sync"
	"testing"
	"time"
)

type liveAutoFake struct {
	autoFake
	recovered int
	policy    *approuting.TUNPolicy
}

func (f *liveAutoFake) RecoverVPN(ctx context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recovered++
	f.unhealthy = false
	return true, ctx.Err()
}
func (f *liveAutoFake) ApplyAppRouting(ctx context.Context, p *approuting.TUNPolicy, persist func() error) error {
	if e := persist(); e != nil {
		return e
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.policy = p.Clone()
	return nil
}
func TestAutomaticLivePolicyAndSameUplinkRecovery(t *testing.T) {
	var mu sync.Mutex
	u := Uplink{Index: 5, Address: "192.0.2.2", Gateway: "192.0.2.1"}
	events := make(chan struct{}, 4)
	var engines []*liveAutoFake
	s := valid()
	s.UplinkMode = "auto"
	b := NewAutomatic(s, func(context.Context, Settings) (Uplink, error) { mu.Lock(); defer mu.Unlock(); return u, nil }, func(s Settings) ManagedEngine {
		f := &liveAutoFake{policy: s.AppRouting.Clone()}
		mu.Lock()
		engines = append(engines, f)
		mu.Unlock()
		return f
	}, func(context.Context) (<-chan struct{}, func(), error) { return events, func() {}, nil }, time.Hour)
	if e := b.Start(context.Background(), b.Config()); e != nil {
		t.Fatal(e)
	}
	defer b.Stop(context.Background())
	awaitAuto(t, b, "ready")
	mu.Lock()
	first := engines[0]
	mu.Unlock()
	p := &approuting.TUNPolicy{Version: 2, Enabled: true, Mode: approuting.OnlySelected}
	if e := b.ApplyAppRouting(context.Background(), p, func() error { return nil }); e != nil {
		t.Fatal(e)
	}
	first.mu.Lock()
	first.unhealthy = true
	first.mu.Unlock()
	events <- struct{}{}
	deadline := time.Now().Add(time.Second)
	for {
		first.mu.Lock()
		n := first.recovered
		first.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("same-uplink recovery rebuilt engine")
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	if len(engines) != 1 {
		t.Fatal("direct owner recreated")
	}
	u.Index = 6
	mu.Unlock()
	events <- struct{}{}
	deadline = time.Now().Add(time.Second)
	for {
		mu.Lock()
		n := len(engines)
		var last *liveAutoFake
		if n > 1 {
			last = engines[n-1]
		}
		mu.Unlock()
		if last != nil {
			if last.policy == nil || !last.policy.Enabled {
				t.Fatal("new engine lost hot policy")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("network change did not rebuild")
		}
		time.Sleep(time.Millisecond)
	}
}
