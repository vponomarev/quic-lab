package windowsclient

import (
	"context"
	"encoding/json"
	"quiclab/internal/approuting"
	"quiclab/internal/vpnclient"
	"quiclab/internal/windowsengine"
	"strings"
	"testing"
)

func TestProfileLibraryLifecycleAndIsolation(t *testing.T) {
	ctx := context.Background()
	b := &liveEditBackend{}
	s := editorSettings()
	s.AppRouting = &approuting.TUNPolicy{Version: 2, Enabled: true, Mode: approuting.OnlySelected}
	store := &settingsMemory{doc: SettingsDocument{Revision: 1, Settings: s}}
	factory := func(windowsengine.Settings) (vpnclient.Backend, vpnclient.Config, error) { return b, config(), nil }
	m, err := NewSettingsManager(store, factory)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	other := editorSettings()
	other.Name = "backup"
	other.Endpoint = "192.0.2.2:443"
	other.Key = "OTHER-SECRET"
	other.Targets = []string{"203.0.113.0/24"}
	other.AppRouting = nil
	q := ProfileChange{ExpectedRevision: 1, Settings: &other, ReplaceKey: true}
	if err = m.SaveProfile(ctx, q); err != nil {
		t.Fatal(err)
	}
	list := m.Profiles()
	if len(list.Profiles) != 1 || list.Revision != 2 || b.stops != 0 || m.Read().Settings.Endpoint != s.Endpoint {
		t.Fatal("saving altered active session")
	}
	id := list.Profiles[0].ID
	raw, _ := json.Marshal(m.Read())
	if strings.Contains(string(raw), "OTHER-SECRET") || strings.Contains(string(raw), "profiles") {
		t.Fatal("read_settings leaked library")
	}
	raw, _ = json.Marshal(list)
	if strings.Contains(string(raw), "SECRET") {
		t.Fatal("list leaked key")
	}
	other.Key = "mutated"
	if store.doc.Profiles[0].Settings.Key != "OTHER-SECRET" {
		t.Fatal("aliased input")
	}
	stale := ProfileChange{ExpectedRevision: 1, ID: id}
	if m.DeleteProfile(ctx, stale) == nil {
		t.Fatal("stale delete")
	}
	activate := ProfileChange{ExpectedRevision: 2, ID: id}
	if err = m.ActivateProfile(ctx, activate); err == nil || err.Error() != "stop_before_apply" {
		t.Fatal("running activation unguarded", err)
	}
	activate.Reconnect = true
	store.fail = true
	if m.ActivateProfile(ctx, activate) == nil || m.Read().Settings.Endpoint != s.Endpoint || m.Read().Revision != 2 {
		t.Fatal("failed persistence changed selected settings")
	}
	store.fail = false
	activate.Reconnect = false
	if err = m.ActivateProfile(ctx, activate); err != nil {
		t.Fatal(err)
	}
	got := m.Read()
	if got.Settings.Endpoint != "192.0.2.2:443" || got.Settings.Targets[0] != s.Targets[0] || got.Settings.AppRouting == nil || !got.Settings.AppRouting.Enabled || store.doc.Settings.Key != "OTHER-SECRET" {
		t.Fatal("activation lost credentials/local policy")
	}
	if m.Snapshot().State != vpnclient.Stopped {
		t.Fatal("stopped activation autostarted")
	}
	normal := draft(m)
	normal.Settings.Name = "edited active"
	if err = m.Apply(ctx, normal); err != nil {
		t.Fatal(err)
	}
	if len(m.Profiles().Profiles) != 1 {
		t.Fatal("normal settings erased library")
	}
	reloaded, err := NewSettingsManager(store, factory)
	if err != nil || len(reloaded.Profiles().Profiles) != 1 {
		t.Fatal("library lost after reload", err)
	}
	del := ProfileChange{ExpectedRevision: m.Read().Revision, ID: id}
	if err = m.DeleteProfile(ctx, del); err != nil {
		t.Fatal(err)
	}
	if len(m.Profiles().Profiles) != 0 || m.Read().Settings.Endpoint != "192.0.2.2:443" {
		t.Fatal("deleting snapshot altered active copy")
	}
}

func TestProfileLibraryBoundsReplaceAndProtocol(t *testing.T) {
	m, store, _ := managerFixture(t)
	ctx := context.Background()
	s := editorSettings()
	if err := m.SaveProfile(ctx, ProfileChange{ExpectedRevision: 1, Settings: &s, ReplaceKey: true}); err != nil {
		t.Fatal(err)
	}
	id := m.Profiles().Profiles[0].ID
	s.Name = "replacement"
	s.Key = "replacement-secret"
	if err := m.SaveProfile(ctx, ProfileChange{ExpectedRevision: 2, ID: id, Settings: &s, ReplaceKey: true}); err != nil {
		t.Fatal(err)
	}
	if len(m.Profiles().Profiles) != 1 || m.Profiles().Profiles[0].Name != "replacement" {
		t.Fatal("replace duplicated")
	}
	store.fail = true
	s.Name = "failed"
	if m.SaveProfile(ctx, ProfileChange{ExpectedRevision: 3, ID: id, Settings: &s, ReplaceKey: true}) == nil || m.Read().Revision != 3 || m.Profiles().Profiles[0].Name != "replacement" {
		t.Fatal("failed save changed library")
	}
	store.fail = false
	for len(m.Profiles().Profiles) < 32 {
		if err := m.SaveProfile(ctx, ProfileChange{ExpectedRevision: m.Read().Revision, Settings: &s, ReplaceKey: true}); err != nil {
			t.Fatal(err)
		}
	}
	if m.SaveProfile(ctx, ProfileChange{ExpectedRevision: m.Read().Revision, Settings: &s, ReplaceKey: true}) == nil {
		t.Fatal("unbounded library")
	}
	h := NewSettingsHandler(m)
	for _, command := range []string{"list_profiles", "read_settings"} {
		r := h.Handle(ctx, Request{Version: 1, ID: "test", Command: command})
		raw, _ := json.Marshal(r)
		if !r.OK || strings.Contains(string(raw), "replacement-secret") {
			t.Fatalf("response %s", command)
		}
	}
	for _, q := range []Request{
		{Version: 1, ID: "a", Command: "delete_profile", Profile: &ProfileChange{ID: "../bad", ExpectedRevision: 3}},
		{Version: 1, ID: "a", Command: "list_profiles", Settings: &SettingsChange{}},
		{Version: 1, ID: "a", Command: "save_profile", Profile: &ProfileChange{}},
		{Version: 1, ID: "a", Command: "status", Profile: &ProfileChange{}},
	} {
		if validRequest(q) {
			t.Fatal("invalid shape accepted", q.Command)
		}
	}
	d := store.doc
	d.Profiles = append(d.Profiles, d.Profiles[0])
	store.doc = d
	if _, err := NewSettingsManager(store, func(windowsengine.Settings) (vpnclient.Backend, vpnclient.Config, error) {
		return &editBackend{}, config(), nil
	}); err == nil {
		t.Fatal("invalid stored library accepted")
	}
}

func TestProfileActivationTransportsAndMergedValidation(t *testing.T) {
	const awg = "[Interface]\nPrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=\nAddress = 10.55.0.2/32\nDNS = 1.1.1.1\nJc = 2\nJmin = 40\nJmax = 70\nS1 = 0\nS2 = 0\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n[Peer]\nPublicKey = AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=\nEndpoint = 192.0.2.2:443\nAllowedIPs = 0.0.0.0/0\n"
	for _, transport := range []string{"quic", "https", "awg", "vless"} {
		t.Run(transport, func(t *testing.T) {
			m, store, _ := managerFixture(t)
			s := editorSettings()
			s.Transport = transport
			s.Endpoint = "192.0.2.2:443"
			s.InterfaceIndex = 9
			s.DNS = "9.9.9.9"
			s.Targets = []string{"203.0.113.0/24"}
			if transport == "awg" || transport == "vless" {
				s.Hostname = ""
				s.Certificate = ""
				s.Key = awg
				if transport == "vless" {
					s.Key = "vless://01234567-89ab-cdef-0123-456789abcdef@192.0.2.2:443?security=tls&sni=test.example&type=ws&path=%2Ftunnel"
				}
			}
			if err := m.SaveProfile(context.Background(), ProfileChange{ExpectedRevision: 1, Settings: &s, ReplaceKey: true}); err != nil {
				t.Fatal(err)
			}
			if err := m.ActivateProfile(context.Background(), ProfileChange{ExpectedRevision: 2, ID: m.Profiles().Profiles[0].ID}); err != nil {
				t.Fatal(err)
			}
			got := store.doc.Settings
			if got.Transport != transport || got.Key != s.Key || got.Hostname != s.Hostname || got.Certificate != s.Certificate || got.InterfaceIndex != 5 || got.DNS != "" || got.Targets[0] != "198.18.0.1/32" {
				t.Fatal("transport or local fields corrupted")
			}
		})
	}
	m, _, b := managerFixture(t)
	s := editorSettings()
	s.Endpoint = "198.18.0.1:443"
	s.Targets = []string{"203.0.113.0/24"}
	if err := m.SaveProfile(context.Background(), ProfileChange{ExpectedRevision: 1, Settings: &s, ReplaceKey: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.failStop = true // A route conflict must be rejected before even trying Stop.
	err := m.ActivateProfile(context.Background(), ProfileChange{ExpectedRevision: 2, ID: m.Profiles().Profiles[0].ID, Reconnect: true})
	if err == nil || err.Error() == "cleanup_required" || m.Snapshot().State != vpnclient.Running || m.Read().Revision != 2 {
		t.Fatal("merged route conflict reached runtime", err)
	}
}

func TestProfileLibraryCancellationAndSize(t *testing.T) {
	m, _, _ := managerFixture(t)
	s := editorSettings()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if m.SaveProfile(ctx, ProfileChange{ExpectedRevision: 1, Settings: &s, ReplaceKey: true}) == nil || m.Read().Revision != 1 {
		t.Fatal("cancelled save")
	}
	s.Key = strings.Repeat("x", MaxFrame)
	if m.SaveProfile(context.Background(), ProfileChange{ExpectedRevision: 1, Settings: &s, ReplaceKey: true}) == nil || len(m.Profiles().Profiles) != 0 || m.Read().Revision != 1 {
		t.Fatal("oversize changed library")
	}
}
