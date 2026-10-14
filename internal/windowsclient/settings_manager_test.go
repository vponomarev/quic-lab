package windowsclient

import (
	"context"
	"encoding/json"
	"errors"
	"quiclab/internal/vpnclient"
	"quiclab/internal/windowsengine"
	"strings"
	"testing"
	"time"
)

type settingsMemory struct {
	doc  SettingsDocument
	fail bool
}

func (s *settingsMemory) Save(_ context.Context, d SettingsDocument) error {
	if s.fail {
		return errors.New("secret write error")
	}
	s.doc = d
	return nil
}
func (s *settingsMemory) Load(context.Context) (*SettingsDocument, error) { d := s.doc; return &d, nil }
func editorSettings() windowsengine.Settings {
	return windowsengine.Settings{Transport: "quic", Endpoint: "192.0.2.1:443", Hostname: "test", Certificate: "certificate", Key: "SECRET-KEY", InterfaceIndex: 5, Targets: []string{"198.18.0.1/32"}}
}

type editBackend struct {
	failStop bool
	wait     chan struct{}
	entered  chan struct{}
}

func (b *editBackend) Start(ctx context.Context, _ vpnclient.Config) error {
	if b.entered != nil {
		close(b.entered)
	}
	if b.wait != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.wait:
		}
	}
	return nil
}
func (b *editBackend) Stop(context.Context) error {
	if b.failStop {
		return errors.New("secret cleanup error")
	}
	return nil
}
func managerFixture(t *testing.T) (*SettingsManager, *settingsMemory, *editBackend) {
	t.Helper()
	b := &editBackend{}
	store := &settingsMemory{doc: SettingsDocument{Revision: 1, Settings: editorSettings()}}
	m, e := NewSettingsManager(store, func(s windowsengine.Settings) (vpnclient.Backend, vpnclient.Config, error) { return b, config(), nil })
	if e != nil {
		t.Fatal(e)
	}
	return m, store, b
}
func draft(m *SettingsManager) SettingsChange {
	v := m.Read()
	return SettingsChange{ExpectedRevision: v.Revision, Settings: v.Settings}
}
func TestSettingsManagerPreservesSecretsAndRevision(t *testing.T) {
	m, s, _ := managerFixture(t)
	c := draft(m)
	c.Settings.Name = "new"
	if e := m.Apply(context.Background(), c); e != nil {
		t.Fatal(e)
	}
	if s.doc.Revision != 2 || s.doc.Settings.Key != "SECRET-KEY" {
		t.Fatal("lost key/revision")
	}
	if e := m.Apply(context.Background(), c); e == nil || e.Error() != "revision_conflict" {
		t.Fatal("stale write", e)
	}
	v := m.Read()
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "SECRET-KEY") || !v.HasKey {
		t.Fatal("read leaks secret")
	}
}
func TestSettingsManagerSaveFailureAndCleanup(t *testing.T) {
	for _, cleanup := range []bool{false, true} {
		m, s, b := managerFixture(t)
		ctx := context.Background()
		if e := m.Start(ctx); e != nil {
			t.Fatal(e)
		}
		c := draft(m)
		c.Reconnect = true
		c.Settings.Name = "new"
		b.failStop = cleanup
		s.fail = !cleanup
		if m.Apply(ctx, c) == nil {
			t.Fatal("failure accepted")
		}
		if m.Read().Revision != 1 || s.doc.Settings.Name != "" {
			t.Fatal("failure committed")
		}
		if !cleanup && m.Snapshot().State != vpnclient.Stopped {
			t.Fatal("save failure left running")
		}
	}
}
func TestSettingsManagerReplacementAndRunningGuard(t *testing.T) {
	m, s, _ := managerFixture(t)
	ctx := context.Background()
	if e := m.Start(ctx); e != nil {
		t.Fatal(e)
	}
	c := draft(m)
	if e := m.Apply(ctx, c); e == nil || e.Error() != "stop_before_apply" {
		t.Fatal(e)
	}
	c.Reconnect = true
	c.ReplaceKey = true
	c.Settings.Key = "replacement"
	if e := m.Apply(ctx, c); e != nil {
		t.Fatal(e)
	}
	if s.doc.Settings.Key != "replacement" || m.Snapshot().State != vpnclient.Running || m.Snapshot().Revision != 2 {
		t.Fatal("replacement failed")
	}
}
func TestSettingsManagerStopCancelsStartup(t *testing.T) {
	m, _, b := managerFixture(t)
	b.wait = make(chan struct{})
	b.entered = make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- m.Start(context.Background()) }()
	<-b.entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := m.Stop(ctx); e != nil {
		t.Fatal(e)
	}
	if <-done == nil {
		t.Fatal("cancelled start succeeded")
	}
	if m.Snapshot().State != vpnclient.Stopped {
		t.Fatal("not stopped")
	}
}
func TestSettingsManagerValidationDoesNotStop(t *testing.T) {
	m, _, _ := managerFixture(t)
	ctx := context.Background()
	m.Start(ctx)
	c := draft(m)
	c.Reconnect = true
	c.Settings.Targets = []string{"0.0.0.0/0"}
	if m.Apply(ctx, c) == nil || m.Snapshot().State != vpnclient.Running {
		t.Fatal("invalid draft changed runtime")
	}
}

func TestSettingsIPCAndConcurrentApply(t *testing.T) {
	m, _, _ := managerFixture(t)
	h := NewSettingsHandler(m)
	ctx := context.Background()
	r := h.Handle(ctx, Request{Version: 1, ID: "a", Command: "read_settings"})
	raw, _ := json.Marshal(r)
	if !r.OK || r.Settings == nil || strings.Contains(string(raw), "SECRET-KEY") || len(r.Capabilities) != 1 {
		t.Fatal("invalid public view")
	}
	c := draft(m)
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- m.Apply(ctx, c) }()
	}
	a, b := <-done, <-done
	if (a == nil) == (b == nil) {
		t.Fatal("expected one concurrent writer")
	}
	if validRequest(Request{Version: 1, ID: "a", Command: "status", Settings: &c}) {
		t.Fatal("mixed payload")
	}
	r = h.Handle(ctx, Request{Version: 1, ID: "a", Command: "apply_config", Config: ptrConfig()})
	if r.OK || r.Error != "unsupported_command" {
		t.Fatal("fixture config accepted by real manager")
	}
}
func ptrConfig() *vpnclient.Config { c := config(); return &c }
