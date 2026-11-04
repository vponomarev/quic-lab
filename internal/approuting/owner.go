package approuting

import (
	"context"
	"net/netip"
	"time"
)

type FlowKey struct {
	Protocol            uint8
	Source, Destination netip.AddrPort
}
type Quality string

const (
	Resolved    Quality = "resolved"
	Shared      Quality = "shared"
	Unknown     Quality = "unknown"
	Unsupported Quality = "unsupported"
)

type ProcessIdentity struct {
	Path    string
	PID     uint32
	Created uint64
}
type Attribution struct {
	Quality Quality
	Reason  string
	Owners  []ProcessIdentity
}
type OwnerResolver interface {
	Resolve(context.Context, FlowKey) Attribution
}
type OwnerRow struct {
	Local, Remote netip.AddrPort
	PID           uint32
}

// UDP remote endpoints are absent from OWNER_PID tables. Include wildcard and
// exact binds, even when an exact bind exists; never guess the first owner.
func OwnerCandidates(f FlowKey, rows []OwnerRow) ([]uint32, string) {
	if f.Protocol != 6 && f.Protocol != 17 {
		return nil, "unsupported_protocol"
	}
	if !f.Source.Addr().Is4() || !f.Destination.Addr().Is4() {
		return nil, "unsupported_address"
	}
	seen := map[uint32]bool{}
	var ids []uint32
	for _, r := range rows {
		match := r.Local == f.Source && r.Remote == f.Destination
		if f.Protocol == 17 {
			match = r.Local.Port() == f.Source.Port() && (r.Local.Addr() == f.Source.Addr() || r.Local.Addr() == netip.IPv4Unspecified())
		}
		if !match {
			continue
		}
		if r.PID == 0 {
			return nil, "owner_unavailable"
		}
		if !seen[r.PID] {
			seen[r.PID] = true
			ids = append(ids, r.PID)
			if len(ids) > 16 {
				return nil, "too_many_owners"
			}
		}
	}
	if len(ids) == 0 {
		return nil, "not_found"
	}
	return ids, ""
}

// The semaphore remains held until the underlying call actually returns. A
// timed-out native call cannot cause unbounded replacement goroutines.
type BoundedResolver struct {
	lookup  func(context.Context, FlowKey) Attribution
	slots   chan struct{}
	timeout time.Duration
}

func NewBoundedResolver(lookup func(context.Context, FlowKey) Attribution, limit int, timeout time.Duration) *BoundedResolver {
	if limit < 1 {
		limit = 1
	}
	if limit > 16 {
		limit = 16
	}
	if timeout <= 0 {
		timeout = 150 * time.Millisecond
	}
	return &BoundedResolver{lookup, make(chan struct{}, limit), timeout}
}
func (r *BoundedResolver) Resolve(ctx context.Context, f FlowKey) Attribution {
	unknown := func(reason string) Attribution { return Attribution{Quality: Unknown, Reason: reason} }
	if ctx.Err() != nil {
		return unknown("canceled")
	}
	select {
	case r.slots <- struct{}{}:
	default:
		return unknown("overloaded")
	}
	run, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	done := make(chan Attribution, 1)
	go func() { defer func() { <-r.slots }(); done <- r.lookup(run, f) }()
	select {
	case <-run.Done():
		if ctx.Err() != nil {
			return unknown("canceled")
		}
		return unknown("timeout")
	case a := <-done:
		if run.Err() != nil {
			if ctx.Err() != nil {
				return unknown("canceled")
			}
			return unknown("timeout")
		}
		return a
	}
}
