//go:build linux && !android

package linuxclient

import (
	"context"
	"os"
	"path/filepath"
	"quiclab/internal/approuting"
	"testing"
)

func TestLinuxApplicationValidationBeforeNetwork(t *testing.T) {
	c := Config{Transport: "quic", Endpoint: "198.18.0.2:443", Targets: []string{"192.0.2.2/32"}, AppRouting: &approuting.TUNPolicy{Version: 2, Enabled: true, Mode: approuting.OnlySelected, Only: []string{"/not-present/quic-test/application"}}}
	if _, e := c.PrepareApplications(); e == nil {
		t.Fatal("missing executable accepted")
	}
	client := &Client{}
	if e := client.Start(context.Background(), c); e == nil || e.Error() != "application routing: executable unavailable or invalid policy" {
		t.Fatal("must fail before network inspection", e)
	}
	if client.Name != "" || client.dev != nil || client.AppRoutingSnapshot() != nil {
		t.Fatal("failed validation created runtime")
	}
	c.AppRouting.Enabled = false
	if p, e := c.PrepareApplications(); e != nil || p != nil {
		t.Fatal("disabled stale paths must be editable", e)
	}
	exe, _ := os.Executable()
	alias := filepath.Join(t.TempDir(), "app-link")
	if e := os.Symlink(exe, alias); e != nil {
		t.Fatal(e)
	}
	c.AppRouting.Enabled = true
	c.AppRouting.Only = []string{alias}
	policy, e := c.PrepareApplications()
	if e != nil {
		t.Fatal(e)
	}
	if policy.Decide(approuting.Attribution{Quality: approuting.Resolved, Owners: []approuting.ProcessIdentity{{Path: exe}}}) != approuting.Tunnel {
		t.Fatal("symlink not canonicalized")
	}
}
