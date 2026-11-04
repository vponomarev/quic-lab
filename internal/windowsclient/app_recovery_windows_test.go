package windowsclient

import (
	"context"
	"quiclab/internal/approuting"
	"testing"
)

func TestSettingsRemovedExecutableKeepsRepairIPC(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		s := editorSettings()
		s.Transport = "vless"
		s.Hostname = ""
		s.Certificate = ""
		s.Endpoint = "192.0.2.1:443"
		s.Key = "vless://01234567-89ab-cdef-0123-456789abcdef@192.0.2.1:443?security=tls&sni=test.example&type=tcp"
		s.AppRouting = &approuting.TUNPolicy{Version: 2, Enabled: enabled, Mode: approuting.OnlySelected, Only: []string{`C:\quic-test-removed-application\game.exe`}}
		store := &settingsMemory{doc: SettingsDocument{Revision: 1, Settings: s}}
		manager, e := NewSettingsManager(store, newSettingsBackend, validateApplicationSettings)
		if e != nil {
			t.Fatalf("stale app prevented service settings startup (enabled=%v): %v", enabled, e)
		}
		response := NewSettingsHandler(manager).Handle(context.Background(), Request{Version: 1, ID: "repair", Command: "read_settings"})
		if !response.OK || response.Settings == nil || response.Settings.Settings.AppRouting.Only[0] != s.AppRouting.Only[0] {
			t.Fatal("repair settings unavailable")
		}
		change := draft(manager)
		change.Settings.AppRouting.Enabled = false
		if e = manager.Apply(context.Background(), change); e != nil {
			t.Fatalf("cannot disable stale rules: %v", e)
		}
		change = draft(manager)
		change.Settings.AppRouting.Enabled = true
		if manager.Validate(change) == nil || manager.Apply(context.Background(), change) == nil {
			t.Fatal("stale rules activated")
		}
		if manager.Read().Settings.AppRouting.Enabled {
			t.Fatal("invalid activation changed saved state")
		}
	}
}
