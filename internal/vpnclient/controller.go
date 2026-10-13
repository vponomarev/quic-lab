// Package vpnclient owns platform-neutral service lifecycle; it never mutates the host network itself.
package vpnclient

import (
	"context"
	"errors"
	"quiclab/internal/vpnmodel"
	"reflect"
	"sync"
	"time"
)

type Config struct {
	Revision uint64
	Profiles vpnmodel.Config
}
type Backend interface {
	Start(context.Context, Config) error
	Stop(context.Context) error
}
type State string

const (
	Stopped  State = "stopped"
	Starting State = "starting"
	Running  State = "running"
	Stopping State = "stopping"
	Failed   State = "failed"
)

type Snapshot struct {
	Runtime  *RuntimeInfo `json:"runtime,omitempty"`
	State    State        `json:"state"`
	Reason   string       `json:"reason,omitempty"`
	Revision uint64       `json:"revision"`
}

var (
	ErrInvalidConfig = errors.New("invalid_config")
	ErrStopping      = errors.New("stop_pending")
	ErrRunning       = errors.New("already_running")
	ErrCleanup       = errors.New("cleanup_required")
	ErrStart         = errors.New("backend_start_failed")
)

type Controller struct {
	backend      Backend
	gate         chan struct{}
	mu           sync.Mutex
	s            Snapshot
	cancel       context.CancelFunc
	dirty        bool
	accepted     Config
	stopsPending int
}

func NewController(b Backend) *Controller {
	return &Controller{backend: b, gate: make(chan struct{}, 1), s: Snapshot{State: Stopped}}
}
func (c *Controller) Snapshot() Snapshot {
	c.mu.Lock()
	s := c.s
	c.mu.Unlock()
	if p, ok := c.backend.(RuntimeProvider); ok {
		info := p.RuntimeInfo()
		s.Runtime = &info
	}
	return s
}
func (c *Controller) lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case c.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *Controller) Start(ctx context.Context, cfg Config) error {
	if c.ValidateConfig(cfg) != nil {
		return ErrInvalidConfig
	}
	c.mu.Lock()
	pending := c.stopsPending != 0
	c.mu.Unlock()
	if pending {
		return ErrStopping
	}
	// Own slices: a UI must not change an accepted config while the backend starts.
	cfg.Profiles.Exits = append([]vpnmodel.Exit(nil), cfg.Profiles.Exits...)
	cfg.Profiles.Profiles = append([]vpnmodel.Profile(nil), cfg.Profiles.Profiles...)
	if err := c.lock(ctx); err != nil {
		return err
	}
	defer func() { <-c.gate }()
	c.mu.Lock()
	if c.stopsPending != 0 {
		c.mu.Unlock()
		return ErrStopping
	}
	if c.s.State == Running {
		same := reflect.DeepEqual(c.accepted, cfg)
		c.mu.Unlock()
		if same {
			return nil
		}
		return ErrRunning
	}
	if c.dirty {
		c.mu.Unlock()
		return ErrCleanup
	}
	run, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.dirty = true
	c.accepted = cfg
	c.s = Snapshot{State: Starting, Revision: cfg.Revision}
	c.mu.Unlock()
	// Caller cancellation applies to startup only. UI disconnection after success must not stop VPN.
	unhook := context.AfterFunc(ctx, cancel)
	err := c.backend.Start(run, cfg)
	unhook()
	// Publish success under the same mutex used by Stop to cancel this run.
	c.mu.Lock()
	if err == nil {
		err = run.Err()
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && c.stopsPending != 0 {
		err = ErrStopping
	}
	if err == nil {
		c.s.State = Running
		c.s.Reason = ""
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	if err != nil {
		cancel()
		cleanupCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		cleanupErr := c.backend.Stop(cleanupCtx)
		done()
		c.mu.Lock()
		c.cancel = nil
		c.dirty = cleanupErr != nil
		c.s.State = Failed
		c.s.Reason = "backend_start_failed"
		if cleanupErr != nil {
			c.s.Reason = "cleanup_required"
		}
		c.mu.Unlock()
		if cleanupErr != nil {
			return ErrCleanup
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrStart
	}
	return ErrStart
}
func (c *Controller) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.stopsPending++
	c.s.State = Stopping
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.stopsPending--; c.mu.Unlock() }()
	if err := c.lock(ctx); err != nil {
		return err
	}
	defer func() { <-c.gate }()
	c.mu.Lock()
	dirty := c.dirty
	c.s.State = Stopping
	c.mu.Unlock()
	var err error
	if dirty {
		err = c.backend.Stop(ctx)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancel = nil
	if err != nil {
		c.s.State = Failed
		c.s.Reason = "cleanup_required"
		return ErrCleanup
	}
	c.dirty = false
	c.s.State = Stopped
	c.s.Reason = ""
	return nil
}

func (c *Controller) ValidateConfig(cfg Config) error {
	if vpnmodel.Validate(cfg.Profiles) != nil {
		return ErrInvalidConfig
	}
	if v, ok := c.backend.(interface{ ValidateConfig(Config) error }); ok {
		if v.ValidateConfig(cfg) != nil {
			return ErrInvalidConfig
		}
	}
	return nil
}
