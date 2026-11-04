package windowsclient

import (
	"context"
	"quiclab/internal/approuting"
	"slices"
	"testing"
)

func TestSettingsApplicationPolicyIsolatedAndCapability(t *testing.T) {
	m, _, _ := managerFixture(t)
	c := draft(m)
	c.Settings.AppRouting = &approuting.TUNPolicy{Version: 2, Enabled: true, Mode: approuting.OnlySelected, Only: []string{`C:\app\game.exe`}, Except: []string{`C:\app\other.exe`}}
	if e := m.Apply(context.Background(), c); e != nil {
		t.Fatal(e)
	}
	c.Settings.AppRouting.Only[0] = `C:\changed.exe`
	view := m.Read()
	view.Settings.AppRouting.Except[0] = `C:\changed.exe`
	if m.Read().Settings.AppRouting.Only[0] != `C:\app\game.exe` || m.Read().Settings.AppRouting.Except[0] != `C:\app\other.exe` {
		t.Fatal("mutable policy escaped settings transaction")
	}
	r := NewSettingsHandler(m).Handle(context.Background(), Request{Version: 1, ID: "apps", Command: "read_settings"})
	if !r.OK || !slices.Contains(r.Capabilities, "app-routing-tun-v2") {
		t.Fatal("missing capability")
	}
}
