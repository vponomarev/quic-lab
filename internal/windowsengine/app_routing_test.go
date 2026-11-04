package windowsengine

import (
	"encoding/json"
	"quiclab/internal/approuting"
	"testing"
)

func TestSettingsApplicationRoutingRoundtripAndIsolation(t *testing.T) {
	s := valid()
	s.AppRouting = &approuting.TUNPolicy{Version: 2, Enabled: true, Mode: approuting.OnlySelected, Only: []string{`C:\app\game.exe`}, Except: []string{`C:\app\other.exe`}}
	raw, _ := json.Marshal(s)
	loaded, e := ParseSettings(raw)
	if e != nil || loaded.AppRouting == nil || !loaded.AppRouting.Enabled {
		t.Fatal(e)
	}
	engine := New(s, nil, nil)
	auto := NewAutomatic(s, nil, nil, nil, 0)
	s.AppRouting.Only[0] = `C:\different.exe`
	if engine.s.AppRouting.Only[0] != `C:\app\game.exe` || auto.s.AppRouting.Only[0] != `C:\app\game.exe` {
		t.Fatal("settings alias")
	}
	if engine.RuntimeInfo().AppRouting == nil || !engine.RuntimeInfo().AppRouting.Enabled {
		t.Fatal("missing routing status")
	}
	loaded.AppRouting.Version = 1
	if loaded.Validate() == nil {
		t.Fatal("WFP policy accepted as TUN")
	}
	loaded.AppRouting.Version = 2
	loaded.AppRouting.Unknown = "random"
	if loaded.Validate() == nil {
		t.Fatal("unknown behavior accepted")
	}
}
