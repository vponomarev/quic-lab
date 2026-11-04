package windowsengine

import (
	"context"
	"errors"
	"quiclab/internal/approuting"
	"reflect"
)

type applicationUpdater interface {
	ApplyAppRouting(context.Context, *approuting.TUNPolicy, func() error) error
}

// SameConnectionSettings excludes only app policy; credentials/routes still require restart.
func SameConnectionSettings(a, b Settings) bool {
	a.AppRouting = nil
	b.AppRouting = nil
	return reflect.DeepEqual(a, b)
}
func compileAppPolicy(p *approuting.TUNPolicy) (*approuting.TUNCompiled, error) {
	if p == nil {
		p = &approuting.TUNPolicy{Version: 2, Mode: approuting.OnlySelected}
	}
	return approuting.CompileTUN(*p)
}
func (e *Engine) ApplyAppRouting(ctx context.Context, p *approuting.TUNPolicy, persist func() error) error {
	p = p.Clone()
	if _, err := compileAppPolicy(p); err != nil {
		return err
	}
	updater, ok := e.g.(applicationUpdater)
	if !ok {
		return errors.New("live_policy_unavailable")
	}
	if err := updater.ApplyAppRouting(ctx, p, persist); err != nil {
		return err
	}
	e.policy.Store(p)
	return nil
}
func (e *Engine) RecoverVPN(ctx context.Context) (bool, error) {
	if r, ok := e.g.(interface {
		RecoverVPN(context.Context) (bool, error)
	}); ok {
		return r.RecoverVPN(ctx)
	}
	return false, nil
}
func (b *Automatic) settings() Settings { s := b.s; s.AppRouting = b.policy.Load().Clone(); return s }

// A queued edit must release the settings-manager gate when Stop cancels it,
// even while reconciliation is still dialing under this operation gate.
func (b *Automatic) lockOperation(ctx context.Context) error {
	select {
	case b.op <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-b.op
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (b *Automatic) ApplyAppRouting(ctx context.Context, p *approuting.TUNPolicy, persist func() error) error {
	p = p.Clone()
	if _, err := compileAppPolicy(p); err != nil {
		return err
	}
	if err := b.lockOperation(ctx); err != nil {
		return err
	}
	defer func() { <-b.op }()
	if b.current != nil {
		updater, ok := b.current.(applicationUpdater)
		if !ok {
			return errors.New("live_policy_unavailable")
		}
		if err := updater.ApplyAppRouting(ctx, p, persist); err != nil {
			return err
		}
	} else if err := persist(); err != nil {
		return err
	}
	b.policy.Store(p)
	return nil
}
