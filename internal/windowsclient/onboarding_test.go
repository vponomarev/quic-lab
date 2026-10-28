package windowsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"quiclab/internal/vpnclient"
	"quiclab/internal/windowsengine"
	"testing"
)

type emptySettingsStore struct{ settingsMemory }

func (s *emptySettingsStore) Load(context.Context) (*SettingsDocument, error) {
	if s.doc.Revision == 0 {
		return nil, nil
	}
	d := s.doc
	return &d, nil
}

func TestFirstRunCanConfigureWithoutStartingVPN(t *testing.T) {
	store := &emptySettingsStore{}
	calls := 0
	factory := func(s windowsengine.Settings) (vpnclient.Backend, vpnclient.Config, error) {
		calls++
		return &editBackend{}, config(), nil
	}
	m, err := NewSettingsManager(store, factory)
	if err != nil {
		t.Fatalf("empty installation must expose settings: %v", err)
	}
	if calls != 0 {
		t.Fatal("unconfigured installation constructed a transport")
	}
	if err = m.Start(context.Background()); err == nil || err.Error() != "config_required" {
		t.Fatalf("empty Start: %v", err)
	}
	if err = m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	view := m.Read()
	if view.Revision != 0 || view.HasKey || view.Settings.RoutingMode != "all_ipv4" || view.Settings.UplinkMode != "auto" {
		t.Fatalf("bad first-run defaults: %+v", view)
	}
	h := NewSettingsHandler(m)
	viewReply := h.Handle(context.Background(), Request{Version: 1, ID: "read", Command: "read_settings"})
	wire, err := json.Marshal(viewReply)
	if err != nil || !bytes.Contains(wire, []byte(`"targets":[]`)) {
		t.Fatalf("first-run wire must provide an iterable empty route list: %s (%v)", wire, err)
	}
	r := h.Handle(context.Background(), Request{Version: 1, ID: "first", Command: "start"})
	if r.OK || r.Error != "config_required" {
		t.Fatalf("protocol lost configuration error: %+v", r)
	}
	change := SettingsChange{ExpectedRevision: 0, Settings: editorSettings(), ReplaceKey: true}
	if err = m.Apply(context.Background(), change); err != nil {
		t.Fatal(err)
	}
	if m.Read().Revision != 1 || !m.Read().HasKey || m.Snapshot().State != vpnclient.Stopped {
		t.Fatal("first save must persist without starting")
	}
	restarted, err := NewSettingsManager(store, factory)
	if err != nil || restarted.Read().Revision != 1 {
		t.Fatalf("restart: %v", err)
	}
}
func TestFirstSaveFailureRetainsUnconfiguredState(t *testing.T) {
	store := &emptySettingsStore{}
	store.fail = true
	m, err := NewSettingsManager(store, func(s windowsengine.Settings) (vpnclient.Backend, vpnclient.Config, error) {
		return &editBackend{}, config(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Apply(context.Background(), SettingsChange{Settings: editorSettings(), ReplaceKey: true})
	if err == nil || m.Read().HasKey || m.Read().Revision != 0 {
		t.Fatal("failed save configured the service")
	}
}

type brokenSettingsStore struct{ emptySettingsStore }

func (*brokenSettingsStore) Load(context.Context) (*SettingsDocument, error) {
	return nil, errors.New("corrupt")
}
func TestBrokenStorageDoesNotBecomeFirstRun(t *testing.T) {
	_, err := NewSettingsManager(&brokenSettingsStore{}, func(s windowsengine.Settings) (vpnclient.Backend, vpnclient.Config, error) {
		t.Fatal("factory called")
		return nil, vpnclient.Config{}, nil
	})
	if err == nil {
		t.Fatal("corruption treated as no profile")
	}
}
