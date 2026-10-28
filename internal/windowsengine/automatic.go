package windowsengine

import (
	"context"
	"encoding/json"
	"errors"
	"quiclab/internal/vpnclient"
	"reflect"
	"sync"
	"time"
)

type Uplink struct {
	Index   uint32   `json:"index"`
	Address string   `json:"address"`
	Gateway string   `json:"gateway"`
	Local   []string `json:"local"`
}

// Only the current start attempt may reuse its discovery result. It is never
// serialized into settings or reused after a network change/retry.
type uplinkSnapshotKey struct{}
type ManagedEngine interface {
	vpnclient.Backend
	Config() vpnclient.Config
	RuntimeInfo() vpnclient.RuntimeInfo
}
type Automatic struct {
	s          Settings
	discover   func(context.Context, Settings) (Uplink, error)
	factory    func(Settings) ManagedEngine
	watch      func(context.Context) (<-chan struct{}, func(), error)
	interval   time.Duration
	mu         sync.Mutex
	cancel     context.CancelFunc
	done       chan struct{}
	info       vpnclient.RuntimeInfo
	remaining  ManagedEngine
	cleanupErr error
}

func NewAutomatic(s Settings, d func(context.Context, Settings) (Uplink, error), f func(Settings) ManagedEngine, w func(context.Context) (<-chan struct{}, func(), error), interval time.Duration) *Automatic {
	return &Automatic{s: s, discover: d, factory: f, watch: w, interval: interval, info: New(s, nil, nil).RuntimeInfo()}
}
func (b *Automatic) Config() vpnclient.Config { return New(b.s, nil, nil).Config() }
func (b *Automatic) ValidateConfig(c vpnclient.Config) error {
	if b.s.Validate() != nil || !reflect.DeepEqual(c, b.Config()) {
		return vpnclient.ErrInvalidConfig
	}
	return nil
}
func (b *Automatic) RuntimeInfo() vpnclient.RuntimeInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	v := b.info
	v.Targets = append([]string(nil), v.Targets...)
	return v
}
func (b *Automatic) set(state string, e ManagedEngine, index uint32) {
	v := New(b.s, nil, nil).RuntimeInfo()
	if e != nil {
		v = e.RuntimeInfo()
	}
	v.RoutingMode = b.s.RoutingMode
	v.NetworkState = state
	v.InterfaceIndex = index
	b.mu.Lock()
	b.info = v
	b.mu.Unlock()
}
func (b *Automatic) Start(ctx context.Context, c vpnclient.Config) error {
	if e := b.ValidateConfig(c); e != nil {
		return e
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		return errors.New("already_running")
	}
	if b.remaining != nil {
		return errors.New("cleanup_required")
	}
	run, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.done = make(chan struct{})
	b.cleanupErr = nil
	b.info.NetworkState = "reconnecting"
	go b.loop(run, b.done)
	return nil
}
func (b *Automatic) Stop(ctx context.Context) error {
	b.mu.Lock()
	cancel, done := b.cancel, b.done
	b.mu.Unlock()
	if cancel != nil {
		cancel()
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.remaining != nil {
		if e := b.remaining.Stop(ctx); e != nil {
			return e
		}
		b.remaining = nil
	}
	b.cleanupErr = nil
	b.cancel = nil
	b.info = New(b.s, nil, nil).RuntimeInfo()
	return nil
}
func (b *Automatic) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	events, unwatch, _ := b.watch(ctx)
	if unwatch != nil {
		defer unwatch()
	}
	var engine ManagedEngine
	var engineCancel context.CancelFunc
	fingerprint := ""
	var index uint32
	cleanup := func() error {
		if engine == nil {
			return nil
		}
		if engineCancel != nil {
			engineCancel()
			engineCancel = nil
		}
		c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if e := engine.Stop(c); e != nil {
			return e
		}
		engine = nil
		return nil
	}
	defer func() {
		e := cleanup()
		b.mu.Lock()
		b.remaining = engine
		b.cleanupErr = e
		b.mu.Unlock()
		if e != nil {
			b.set("cleanup_required", engine, index)
		}
	}()
	reconcile := func() bool {
		query, cancel := context.WithTimeout(ctx, 10*time.Second)
		u, err := b.discover(query, b.s)
		cancel()
		raw, _ := json.Marshal(u)
		next := string(raw)
		if err == nil && engine != nil && fingerprint == next && engineHealthy(engine) {
			return true
		}
		b.set("reconnecting", nil, u.Index)
		if e := cleanup(); e != nil {
			b.set("cleanup_required", engine, index)
			return false
		}
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			b.set("waiting_network", nil, 0)
			return true
		}
		s := b.s
		s.InterfaceIndex = u.Index
		s.UplinkMode = "manual"
		engine = b.factory(s)
		index = u.Index
		u.Local = append([]string(nil), u.Local...)
		start, cancel := context.WithCancel(context.WithValue(ctx, uplinkSnapshotKey{}, u))
		engineCancel = cancel
		timer := time.AfterFunc(25*time.Second, cancel)
		err = engine.Start(start, engine.Config())
		timer.Stop()
		if err == nil {
			err = start.Err()
		}
		if err != nil {
			if e := cleanup(); e != nil {
				b.set("cleanup_required", engine, index)
				return false
			}
			b.set("reconnecting", nil, index)
			return true
		}
		fingerprint = next
		b.set("ready", engine, index)
		return true
	}
	if !reconcile() {
		return
	}
	poll := time.NewTicker(b.interval)
	defer poll.Stop()
	stats := time.NewTicker(time.Second)
	defer stats.Stop()
	retry := time.NewTicker(3 * time.Second)
	defer retry.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			timer := time.NewTimer(150 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			for len(events) > 0 {
				<-events
			}
			if !reconcile() {
				return
			}
		case <-poll.C:
			if !reconcile() {
				return
			}
		case <-retry.C:
			if (engine == nil || !engineHealthy(engine)) && !reconcile() {
				return
			}
		case <-stats.C:
			if engine != nil {
				b.set("ready", engine, index)
			}
		}
	}
}

func engineHealthy(e ManagedEngine) bool {
	if p, ok := e.(interface{ Healthy() bool }); ok {
		return p.Healthy()
	}
	return true
}
