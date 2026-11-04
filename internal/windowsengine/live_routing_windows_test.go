//go:build windows

package windowsengine

import (
	"context"
	"errors"
	"os"
	"quiclab/internal/approuting"
	"quiclab/mobile"
	"testing"
)

// Native persistence/publication without installing a TUN or changing host routes.
func TestNativeLivePolicyTransaction(t *testing.T) {
	p, _ := compileAppPolicy(nil)
	r, err := mobile.NewDesktopAppRouter(p, approuting.NewWindowsOwnerResolver(), interfaceBinder(1), mobile.NewGateway(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	g := &NativeGateway{router: r}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	policy := &approuting.TUNPolicy{Version: 2, Enabled: true, Mode: approuting.OnlySelected, Only: []string{exe}}
	failed := errors.New("store failed")
	if err := g.ApplyAppRouting(context.Background(), policy, func() error { return failed }); !errors.Is(err, failed) {
		t.Fatal(err)
	}
	if r.Snapshot().Enabled || g.recovery {
		t.Fatal("failed persistence published policy")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.ApplyAppRouting(ctx, policy, func() error { t.Fatal("cancelled save"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := g.ApplyAppRouting(context.Background(), policy, func() error {
		if r.Snapshot().Enabled {
			t.Fatal("published before persistence")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !r.Snapshot().Enabled || !g.recovery {
		t.Fatal("policy not live")
	}
	if handled, err := g.RecoverVPN(ctx); !handled || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled recovery: %v %v", handled, err)
	}
	g.router = nil
	if err := g.ApplyAppRouting(context.Background(), policy, func() error { t.Fatal("save after stop"); return nil }); err == nil {
		t.Fatal("stopped gateway accepted update")
	}
}
