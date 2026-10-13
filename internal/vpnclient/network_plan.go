package vpnclient

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Value is an exact platform-readable network value, never a credential.
type Value struct {
	Present bool   `json:"present"`
	Data    string `json:"data,omitempty"`
}
type Change struct {
	Resource string `json:"resource"`
	Before   Value  `json:"before"`
	After    Value  `json:"after"`
}
type NetworkPlan struct {
	Owner   string
	Changes []Change
}

// Network operations are serialized by Controller. The adapter must verify resource ownership
// and compare immediately before writing. ErrConflict guarantees no mutation occurred.
// Ownership must remain provable for applied entries (an adapter-owned resource or OS receipt);
// matching Data alone is not proof for shared resources. Never implement this by restoring a full host snapshot.
type Network interface {
	Read(context.Context, string) (Value, error)
	CompareAndSwap(context.Context, string, Value, Value) error
}

var (
	ErrConflict = errors.New("network_state_conflict")
	ErrApply    = errors.New("network_apply_failed")
	ErrRollback = errors.New("network_rollback_failed")
	ErrPending  = errors.New("network_recovery_required")
)
var ownerID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

func (p NetworkPlan) validate() error {
	if !ownerID.MatchString(p.Owner) || len(p.Changes) == 0 || len(p.Changes) > 256 {
		return ErrApply
	}
	seen := map[string]bool{}
	for _, c := range p.Changes {
		if !strings.HasPrefix(c.Resource, p.Owner+"/") || len(c.Resource) > 512 || seen[c.Resource] || strings.Contains(c.Resource, "..") || strings.ContainsAny(c.Resource, "\\\r\n") {
			return ErrApply
		}
		seen[c.Resource] = true
		for _, v := range []Value{c.Before, c.After} {
			if len(v.Data) > 4096 || (!v.Present && v.Data != "") {
				return ErrApply
			}
		}
	}
	return nil
}
func (p NetworkPlan) Apply(ctx context.Context, n Network, s JournalStore) error {
	if err := p.validate(); err != nil {
		return err
	}
	old, err := s.Load(ctx)
	if err != nil {
		return ErrJournal
	}
	if _, err = EncodeJournal(old); err != nil {
		return err
	}
	for _, e := range old.Entries {
		if e.Intent && !e.Reverted {
			return ErrPending
		}
	}
	j := Journal{Version: 1, Owner: p.Owner}
	for _, c := range p.Changes {
		j.Entries = append(j.Entries, JournalEntry{Change: c})
	}
	if s.Save(ctx, j) != nil {
		return ErrJournal
	}
	fail := func(cause error) error {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return errors.Join(cause, Rollback(cleanup, n, s))
	}
	for i, c := range p.Changes {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		j.Entries[i].Intent = true
		if s.Save(ctx, j) != nil {
			return fail(ErrJournal)
		}
		if err := n.CompareAndSwap(ctx, c.Resource, c.Before, c.After); err != nil {
			if errors.Is(err, ErrConflict) {
				j.Entries[i].Reverted = true // definitive failure: this change did not happen
				if s.Save(ctx, j) != nil {
					return fail(ErrJournal)
				}
			}
			return fail(ErrApply)
		}
		j.Entries[i].Applied = true
		if s.Save(ctx, j) != nil {
			return fail(ErrJournal)
		}
	}
	return nil
}

// Recovery handles the ambiguous crash window: intent recorded, mutation possibly applied.
// Without a durable success record, matching After is ambiguous and is never undone.
// A foreign value is retained and the journal remains unresolved for explicit recovery.
func Rollback(ctx context.Context, n Network, s JournalStore) error {
	j, err := s.Load(ctx)
	if err != nil {
		return ErrJournal
	}
	if _, err = EncodeJournal(j); err != nil {
		return err
	}
	var failures []error
	for i := len(j.Entries) - 1; i >= 0; i-- {
		e := &j.Entries[i]
		if !e.Intent || e.Reverted {
			continue
		}
		if ctx.Err() != nil {
			return errors.Join(append(failures, ctx.Err())...)
		}
		current, err := n.Read(ctx, e.Change.Resource)
		if err != nil {
			failures = append(failures, ErrRollback)
			continue
		}
		switch current {
		case e.Change.Before:
		case e.Change.After:
			if !e.Applied {
				failures = append(failures, ErrConflict)
				continue
			}
			if err = n.CompareAndSwap(ctx, e.Change.Resource, e.Change.After, e.Change.Before); err != nil {
				failures = append(failures, ErrConflict)
				continue
			}
		default:
			failures = append(failures, ErrConflict)
			continue
		}
		e.Reverted = true
		if s.Save(ctx, j) != nil {
			return errors.Join(append(failures, ErrJournal)...)
		}
	}
	return errors.Join(failures...)
}
