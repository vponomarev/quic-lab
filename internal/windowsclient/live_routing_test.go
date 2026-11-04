package windowsclient

import (
	"context"
	"errors"
	"quiclab/internal/approuting"
	"quiclab/internal/vpnclient"
	"quiclab/internal/windowsengine"
	"sync/atomic"
	"testing"
	"time"
)

type liveEditBackend struct {
	editBackend
	policy *approuting.TUNPolicy
	stops  int
}

type cancelRecoveryEngine struct {
	editBackend
	unhealthy           atomic.Bool
	recovering, release chan struct{}
}

func (b *cancelRecoveryEngine) Config() vpnclient.Config {
	return windowsengine.New(editorSettings(), nil, nil).Config()
}
func (b *cancelRecoveryEngine) RuntimeInfo() vpnclient.RuntimeInfo {
	return vpnclient.RuntimeInfo{Mode: "vpn"}
}
func (b *cancelRecoveryEngine) Healthy() bool { return !b.unhealthy.Load() }
func (b *cancelRecoveryEngine) RecoverVPN(ctx context.Context) (bool, error) {
	close(b.recovering)
	select {
	case <-ctx.Done():
	case <-b.release:
	}
	return true, ctx.Err()
}

type observedLiveApply struct {
	*windowsengine.Automatic
	applying chan struct{}
}

func (b *observedLiveApply) ApplyAppRouting(ctx context.Context, p *approuting.TUNPolicy, persist func() error) error {
	close(b.applying)
	return b.Automatic.ApplyAppRouting(ctx, p, persist)
}
func TestSettingsStopCancelsLiveApplyWaitingForRecovery(t *testing.T) {
	s := editorSettings()
	s.UplinkMode = "auto"
	f := &cancelRecoveryEngine{recovering: make(chan struct{}), release: make(chan struct{})}
	events := make(chan struct{}, 1)
	a := windowsengine.NewAutomatic(s, func(context.Context, windowsengine.Settings) (windowsengine.Uplink, error) {
		return windowsengine.Uplink{Index: 5, Address: "192.0.2.2", Gateway: "192.0.2.1"}, nil
	}, func(windowsengine.Settings) windowsengine.ManagedEngine { return f }, func(context.Context) (<-chan struct{}, func(), error) { return events, func() {}, nil }, time.Hour)
	b := &observedLiveApply{Automatic: a, applying: make(chan struct{})}
	store := &settingsMemory{doc: SettingsDocument{Revision: 1, Settings: s}}
	m, err := NewSettingsManager(store, func(windowsengine.Settings) (vpnclient.Backend, vpnclient.Config, error) { return b, b.Config(), nil })
	if err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())
	defer close(f.release)
	if err = m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for a.RuntimeInfo().NetworkState != "ready" {
		if time.Now().After(deadline) {
			t.Fatal("not ready")
		}
		time.Sleep(time.Millisecond)
	}
	f.unhealthy.Store(true)
	events <- struct{}{}
	select {
	case <-f.recovering:
	case <-time.After(2 * time.Second):
		t.Fatal("no recovery")
	}
	c := draft(m)
	c.Settings.AppRouting = &approuting.TUNPolicy{Version: 2, Enabled: true, Mode: approuting.OnlySelected}
	applied := make(chan error, 1)
	go func() { applied <- m.Apply(context.Background(), c) }()
	select {
	case <-b.applying:
	case <-time.After(time.Second):
		t.Fatal("no live apply")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = m.Stop(stopCtx); err != nil {
		t.Fatalf("Stop could not cancel queued live apply/recovery: %v", err)
	}
	select {
	case err = <-applied:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("apply: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("apply stuck")
	}
	if store.doc.Revision != 1 || m.Read().Revision != 1 || m.Snapshot().State != vpnclient.Stopped {
		t.Fatal("cancelled edit persisted or engine not stopped")
	}
}

func (b *liveEditBackend) Stop(ctx context.Context) error { b.stops++; return b.editBackend.Stop(ctx) }
func (b *liveEditBackend) ApplyAppRouting(ctx context.Context, p *approuting.TUNPolicy, persist func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := persist(); err != nil {
		return err
	}
	b.policy = p.Clone()
	return nil
}
func TestSettingsLiveApplyAndPersistenceFailure(t *testing.T) {
	b := &liveEditBackend{}
	store := &settingsMemory{doc: SettingsDocument{Revision: 1, Settings: editorSettings()}}
	m, err := NewSettingsManager(store, func(s windowsengine.Settings) (vpnclient.Backend, vpnclient.Config, error) { return b, config(), nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	c := draft(m)
	c.Settings.AppRouting = &approuting.TUNPolicy{Version: 2, Enabled: true, Mode: approuting.OnlySelected, Only: []string{`C:\app\game.exe`}}
	if err = m.Apply(ctx, c); err != nil {
		t.Fatal(err)
	}
	if b.stops != 0 || m.Snapshot().Revision != 2 || b.policy == nil || !b.policy.Enabled {
		t.Fatal("hot apply restarted or did not publish")
	}
	c = draft(m)
	c.Settings.AppRouting.Enabled = false
	store.fail = true
	if m.Apply(ctx, c) == nil || !b.policy.Enabled || m.Read().Revision != 2 || b.stops != 0 {
		t.Fatal("failed save altered live policy")
	}
	store.fail = false
	c.Settings.Name = "connection change"
	if err = m.Apply(ctx, c); err == nil || err.Error() != "stop_before_apply" {
		t.Fatal("non-policy change was hot", err)
	}
}
