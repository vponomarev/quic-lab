//go:build linux && !android

package linuxclient

import (
	"context"
	"errors"
	"quiclab/internal/approuting"
	"quiclab/mobile"
	"reflect"
	"time"
)

func cloneConfig(c Config) Config {
	c.Targets = append([]string(nil), c.Targets...)
	c.AppRouting = c.AppRouting.Clone()
	return c
}
func (c *Client) ReloadApplications(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	policy, err := cfg.PrepareApplications()
	if err != nil {
		return err
	}
	if policy == nil {
		policy, _ = approuting.CompileLinuxTUN(approuting.TUNPolicy{Version: 2, Mode: approuting.OnlySelected})
	}
	c.op.Lock()
	defer c.op.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dev == nil || c.router == nil {
		return errors.New("client not running")
	}
	before, after := cloneConfig(c.requested), cloneConfig(cfg)
	before.AppRouting = nil
	after.AppRouting = nil
	if !reflect.DeepEqual(before, after) {
		return errors.New("reload accepts application rules only; connection changes require restart")
	}
	if err := c.router.UpdatePolicy(policy); err != nil {
		return err
	}
	c.requested = cloneConfig(cfg)
	c.recovery = c.recovery || policy.Enabled()
	return nil
}
func (c *Client) RecoveryEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recovery && c.router != nil
}

func (c *Client) RecoverVPN(ctx context.Context) error {
	c.op.Lock()
	defer c.op.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	r, old, cfg := c.router, c.gateway, cloneConfig(c.effective)
	enabled := c.recovery
	c.mu.Unlock()
	if !enabled || r == nil || old == nil {
		return errors.New("VPN recovery unavailable")
	}
	if err := r.ReplaceVPN(nil); err != nil {
		return err
	}
	if c.vpnCancel != nil {
		c.vpnCancel()
		c.vpnCancel = nil
	}
	old.Stop()
	payload, err := cfg.GatewayJSON()
	if err != nil {
		return err
	}
	next := mobile.NewGateway(c.Sink)
	run, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(25*time.Second, cancel)
	err = next.StartLinuxContext(run, payload, Binder(cfg.Interface))
	timer.Stop()
	if err == nil {
		err = run.Err()
	}
	if err != nil {
		cancel()
		next.Stop()
		return errors.New("VPN recovery failed")
	}
	if err = r.ReplaceVPN(next); err != nil {
		cancel()
		next.Stop()
		return err
	}
	c.vpnCancel = cancel
	c.mu.Lock()
	c.gateway = next
	c.mu.Unlock()
	return nil
}
