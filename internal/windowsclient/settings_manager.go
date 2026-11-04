package windowsclient

import (
	"context"
	"errors"
	"quiclab/internal/approuting"
	"quiclab/internal/exitcontrol"
	"quiclab/internal/vpnclient"
	"quiclab/internal/windowsengine"
	"sync"
)

type SettingsDocument struct {
	Profiles []SavedProfile         `json:"profiles,omitempty"`
	Revision uint64                 `json:"revision"`
	Settings windowsengine.Settings `json:"settings"`
}
type SettingsView struct {
	SettingsDocument
	HasKey           bool `json:"has_key"`
	HasServerControl bool `json:"has_server_control"`
}
type SettingsChange struct {
	ReplaceServerControl bool                   `json:"replace_server_control"`
	ExpectedRevision     uint64                 `json:"expected_revision"`
	Settings             windowsengine.Settings `json:"settings"`
	ReplaceKey           bool                   `json:"replace_key"`
	Reconnect            bool                   `json:"reconnect"`
}
type SettingsStore interface {
	Load(context.Context) (*SettingsDocument, error)
	Save(context.Context, SettingsDocument) error
}
type BackendFactory func(windowsengine.Settings) (vpnclient.Backend, vpnclient.Config, error)
type SettingsManager struct {
	exitClient exitcontrol.Client
	gate       chan struct{}
	mu         sync.RWMutex
	doc        SettingsDocument
	store      SettingsStore
	factory    BackendFactory
	validate   func(windowsengine.Settings) error
	controller *vpnclient.Controller
	backend    vpnclient.Backend
	config     vpnclient.Config
	active     uint64
	cancel     context.CancelFunc
	stops      int
}

func cloneSettings(s windowsengine.Settings) windowsengine.Settings {
	s.ServerControl = s.ServerControl.Clone()
	s.Targets = append([]string{}, s.Targets...)
	s.AppRouting = s.AppRouting.Clone()
	return s
}
func NewSettingsManager(store SettingsStore, factory BackendFactory, validators ...func(windowsengine.Settings) error) (*SettingsManager, error) {
	var validate func(windowsengine.Settings) error
	if len(validators) > 1 {
		return nil, errors.New("invalid_config")
	}
	if len(validators) == 1 {
		validate = validators[0]
	}
	d, e := store.Load(context.Background())
	if e != nil {
		return nil, errors.New("config_store_failed")
	}
	if d == nil {
		return &SettingsManager{gate: make(chan struct{}, 1), store: store, factory: factory, validate: validate,
			doc:        SettingsDocument{Settings: windowsengine.Settings{Transport: "quic", RoutingMode: "all_ipv4", UplinkMode: "auto", DNS: "1.1.1.1", Targets: []string{}}},
			controller: vpnclient.NewController(unconfiguredBackend{})}, nil
	}
	if !validDocument(*d) {
		return nil, errors.New("config_store_failed")
	}
	b, c, e := factory(cloneSettings(d.Settings))
	if e != nil {
		return nil, errors.New("invalid_config")
	}
	*d = cloneDocument(*d)
	return &SettingsManager{gate: make(chan struct{}, 1), doc: *d, store: store, factory: factory, validate: validate, controller: vpnclient.NewController(b), backend: b, config: c}, nil
}
func (m *SettingsManager) Read() SettingsView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d := m.doc
	d.Profiles = nil // Credentials in saved snapshots never leave the service.
	d.Settings = cloneSettings(d.Settings)
	has := d.Settings.Key != ""
	d.Settings.Key = ""
	hasControl := d.Settings.ServerControl != nil
	if hasControl {
		d.Settings.ServerControl.Token = ""
	}
	return SettingsView{SettingsDocument: d, HasKey: has, HasServerControl: hasControl}
}
func (m *SettingsManager) Snapshot() vpnclient.Snapshot {
	m.mu.RLock()
	c, active := m.controller, m.active
	m.mu.RUnlock()
	s := c.Snapshot()
	s.Revision = active
	return s
}
func (m *SettingsManager) lock(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	select {
	case m.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *SettingsManager) begin(ctx context.Context) (context.Context, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stops != 0 {
		return nil, nil, vpnclient.ErrStopping
	}
	run, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	return run, func() { cancel(); m.mu.Lock(); m.cancel = nil; m.mu.Unlock() }, nil
}
func (m *SettingsManager) resolve(c SettingsChange) (windowsengine.Settings, error) {
	m.mu.RLock()
	d := m.doc
	m.mu.RUnlock()
	if c.ExpectedRevision != d.Revision {
		return windowsengine.Settings{}, errors.New("revision_conflict")
	}
	s := cloneSettings(c.Settings)
	if !c.ReplaceServerControl {
		if c.ReplaceKey && s.Key != d.Settings.Key && d.Settings.ServerControl != nil {
			return s, errors.New("invalid_config")
		}
		old, next := d.Settings.ServerControl, s.ServerControl
		if (old == nil) != (next == nil) || (next != nil && (next.Token != "" || next.ConfigURL != old.ConfigURL || next.DeviceID != old.DeviceID)) {
			return s, errors.New("invalid_config")
		}
		s.ServerControl = old.Clone()
	}
	if !c.ReplaceKey {
		if s.Key != "" {
			return s, errors.New("invalid_config")
		}
		s.Key = d.Settings.Key
	}
	if e := s.Validate(); e != nil {
		return s, e
	}
	if m.validate != nil {
		if e := m.validate(s); e != nil {
			return s, e
		}
	}
	return s, nil
}
func (m *SettingsManager) Validate(c SettingsChange) error {
	s, e := m.resolve(c)
	if e != nil {
		return e
	}
	if _, _, e = m.factory(s); e != nil {
		return errors.New("invalid_config")
	}
	return nil
}
func (m *SettingsManager) Start(ctx context.Context) error {
	if e := m.lock(ctx); e != nil {
		return e
	}
	defer func() { <-m.gate }()
	run, done, e := m.begin(ctx)
	if e != nil {
		return e
	}
	defer done()
	m.mu.Lock()
	if m.doc.Revision == 0 {
		m.mu.Unlock()
		return errors.New("config_required")
	}
	ctrl, c := m.controller, m.config
	s := cloneSettings(m.doc.Settings)
	m.mu.Unlock()
	if m.validate != nil {
		if e := m.validate(s); e != nil {
			return e
		}
	}
	m.mu.Lock()
	m.active = m.doc.Revision
	m.mu.Unlock()
	return ctrl.Start(run, c)
}
func (m *SettingsManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	m.stops++
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.stops--; m.mu.Unlock() }()
	if e := m.lock(ctx); e != nil {
		return e
	}
	defer func() { <-m.gate }()
	m.mu.RLock()
	c := m.controller
	m.mu.RUnlock()
	return c.Stop(ctx)
}
func (m *SettingsManager) Apply(ctx context.Context, c SettingsChange) error {
	if e := m.lock(ctx); e != nil {
		return e
	}
	defer func() { <-m.gate }()
	return m.applyLocked(ctx, c)
}
func (m *SettingsManager) applyLocked(ctx context.Context, c SettingsChange) error {
	run, done, e := m.begin(ctx)
	if e != nil {
		return e
	}
	defer done()
	s, e := m.resolve(c)
	if e != nil {
		return e
	}
	b, cfg, e := m.factory(s)
	if e != nil {
		return errors.New("invalid_config")
	}
	m.mu.RLock()
	ctrl := m.controller
	m.mu.RUnlock()
	state := ctrl.Snapshot().State
	if state == vpnclient.Running && !c.Reconnect {
		m.mu.RLock()
		previous, backend := m.doc.Settings, m.backend
		m.mu.RUnlock()
		if windowsengine.SameConnectionSettings(previous, s) {
			if updater, ok := backend.(interface {
				ApplyAppRouting(context.Context, *approuting.TUNPolicy, func() error) error
			}); ok {
				d := m.documentWith(s, c.ExpectedRevision+1)
				if d.Revision == 0 {
					return errors.New("revision_conflict")
				}
				if err := updater.ApplyAppRouting(run, s.AppRouting, func() error {
					if err := m.store.Save(run, d); err != nil {
						return errors.New("config_store_failed")
					}
					return nil
				}); err != nil {
					return err
				}
				m.mu.Lock()
				m.doc = d
				m.active = d.Revision
				m.mu.Unlock()
				return nil
			}
		}
	}
	if state != vpnclient.Stopped && !c.Reconnect {
		return errors.New("stop_before_apply")
	}
	if e = ctrl.Stop(run); e != nil {
		return e
	}
	d := m.documentWith(s, c.ExpectedRevision+1)
	if d.Revision == 0 {
		return errors.New("revision_conflict")
	}
	if e = m.store.Save(run, d); e != nil {
		return errors.New("config_store_failed")
	}
	m.mu.Lock()
	m.doc = d
	m.controller = vpnclient.NewController(b)
	m.backend = b
	m.config = cfg
	m.active = 0
	ctrl = m.controller
	m.mu.Unlock()
	if c.Reconnect && state == vpnclient.Running {
		if e = run.Err(); e != nil {
			return e
		}
		m.mu.Lock()
		m.active = d.Revision
		m.mu.Unlock()
		return ctrl.Start(run, cfg)
	}
	return nil
}
