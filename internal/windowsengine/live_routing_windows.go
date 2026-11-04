//go:build windows

package windowsengine

import (
	"context"
	"errors"
	"quiclab/internal/approuting"
	"time"
)

func (g *NativeGateway) ApplyAppRouting(ctx context.Context, p *approuting.TUNPolicy, persist func() error) error {
	canonical, err := approuting.CanonicalTUNPolicy(p)
	if err != nil {
		return &FieldError{Field: "app_routing", Code: "unavailable_executable"}
	}
	compiled, err := compileAppPolicy(canonical)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if g.router == nil {
		return errors.New("live_policy_unavailable")
	}
	if err := persist(); err != nil {
		return err
	}
	if err := g.router.UpdatePolicy(compiled); err != nil {
		return err
	}
	g.recovery = g.recovery || compiled.Enabled()
	return nil
}

func (g *NativeGateway) RecoverVPN(ctx context.Context) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.recovery || g.router == nil {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	if err := g.router.ReplaceVPN(nil); err != nil {
		return true, err
	}
	if g.vpnCancel != nil {
		g.vpnCancel()
		g.vpnCancel = nil
	}
	g.g.Stop()
	run, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(25*time.Second, cancel)
	err := g.g.StartWindowsContext(run, g.settings.GatewayJSON(), interfaceBinder(g.settings.InterfaceIndex))
	timer.Stop()
	if err == nil {
		err = run.Err()
	}
	if err != nil {
		cancel()
		g.g.Stop()
		return true, errors.New("vpn_recovery_failed")
	}
	g.vpnCancel = cancel
	return true, g.router.ReplaceVPN(g.g)
}
