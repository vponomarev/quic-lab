//go:build linux && !android

package linuxclient

import (
	"os"
	"quiclab/internal/approuting"
	"quiclab/mobile"
	"testing"
)

func TestLinuxReloadOnlyApplicationRules(t *testing.T) {
	cfg := Config{Transport: "quic", Endpoint: "198.18.0.2:443", Targets: []string{"192.0.2.2/32"}}
	disabled, _ := approuting.CompileLinuxTUN(approuting.TUNPolicy{Version: 2, Mode: approuting.OnlySelected})
	router, err := mobile.NewDesktopAppRouter(disabled, approuting.NewLinuxOwnerResolver(), Binder("fixture"), mobile.NewGateway(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{dev: f, router: router, requested: cloneConfig(cfg)}
	defer c.Stop()
	exe, _ := os.Executable()
	cfg.AppRouting = &approuting.TUNPolicy{Version: 2, Enabled: true, Mode: approuting.OnlySelected, Only: []string{exe}}
	if err := c.ReloadApplications(cfg); err != nil {
		t.Fatal(err)
	}
	if !c.AppRoutingSnapshot().Enabled || !c.RecoveryEnabled() {
		t.Fatal("not live")
	}
	cfg.AppRouting.Only[0] = "/missing/app"
	if c.ReloadApplications(cfg) == nil || !c.AppRoutingSnapshot().Enabled {
		t.Fatal("bad policy changed runtime")
	}
	cfg.AppRouting = nil
	cfg.Targets = []string{"192.0.2.3/32"}
	if c.ReloadApplications(cfg) == nil || !c.AppRoutingSnapshot().Enabled {
		t.Fatal("changed routes accepted")
	}
	cfg.Targets = []string{"192.0.2.2/32"}
	if err := c.ReloadApplications(cfg); err != nil {
		t.Fatal(err)
	}
	if c.AppRoutingSnapshot().Enabled || !c.RecoveryEnabled() {
		t.Fatal("disabled policy lost surviving direct lifetime")
	}
}
