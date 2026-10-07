package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"quiclab/internal/trafficbudget"
	"sync"
)

// TrafficBudget is owned by the whole VPN run, never by an individual Gateway.
// Every cellular transport in this run shares this meter, including draining paths.
type TrafficBudget struct {
	transferMu sync.Mutex
	grants     map[string]bool
	transfers  map[string]context.CancelFunc
	ledger     *trafficbudget.Ledger
	meter      *trafficbudget.Meter
}

func NewTrafficBudget(epoch string, limit int64) (*TrafficBudget, error) {
	if epoch == "" || limit < 0 {
		return nil, errors.New("budget requires an epoch and a nonnegative limit")
	}
	l := trafficbudget.New(epoch, uint64(limit))
	return &TrafficBudget{ledger: l, meter: trafficbudget.NewMeter(l)}, nil
}
func (b *TrafficBudget) Snapshot() string {
	s := b.ledger.Snapshot()
	s.Blocked = !b.CellAllowed()
	var overrun uint64
	if s.Limit > 0 && s.Used > s.Limit {
		overrun = s.Used - s.Limit
	}
	raw, _ := json.Marshal(struct {
		trafficbudget.Snapshot
		Overrun uint64 `json:"overrun"`
	}{s, overrun})
	return string(raw)
}

// GrantTransfer permits only a single claimed service operation. It never resets
// the run ledger or permits a VPN socket to reopen.
func (b *TrafficBudget) GrantTransfer(id string) error {
	if !validTransferID(id) {
		return errors.New("invalid transfer ID")
	}
	b.transferMu.Lock()
	defer b.transferMu.Unlock()
	if b.transfers[id] != nil {
		return errors.New("cancel transfer before granting restart")
	}
	if b.grants == nil {
		b.grants = make(map[string]bool)
	}
	b.grants[id] = true
	return nil
}
func (b *TrafficBudget) RevokeTransfer(id string) {
	b.transferMu.Lock()
	delete(b.grants, id)
	cancel := b.transfers[id]
	if cancel != nil {
		cancel()
	}
	b.transferMu.Unlock()
}
func (b *TrafficBudget) transferGranted(id string) bool {
	b.transferMu.Lock()
	defer b.transferMu.Unlock()
	return b.grants[id]
}
func (b *TrafficBudget) claimTransfer(id string, cancel context.CancelFunc) (bool, error) {
	b.transferMu.Lock()
	defer b.transferMu.Unlock()
	if b.transfers[id] != nil {
		return false, errors.New("transfer already active")
	}
	if b.transfers == nil {
		b.transfers = make(map[string]context.CancelFunc)
	}
	grant := b.grants[id]
	delete(b.grants, id)
	b.transfers[id] = cancel
	return grant, nil
}
func (b *TrafficBudget) finishTransfer(id string) {
	b.transferMu.Lock()
	delete(b.transfers, id)
	delete(b.grants, id)
	b.transferMu.Unlock()
}

// Invalid attempts must not retain pending consent or interrupt another active
// transfer that happens to have the same operation ID.
func (b *TrafficBudget) discardPendingGrant(id string) {
	b.transferMu.Lock()
	delete(b.grants, id)
	b.transferMu.Unlock()
}

// RestoreTrafficBudget resumes committed usage. In-flight reservations die with
// the process; one-shot service-transfer grants are deliberately not restored.
func RestoreTrafficBudget(raw string) (*TrafficBudget, error) {
	var s trafficbudget.Snapshot
	if e := json.Unmarshal([]byte(raw), &s); e != nil {
		return nil, e
	}
	if s.Epoch == "" || s.Limit > uint64(1<<63-1) || s.Used > uint64(1<<63-1) {
		return nil, errors.New("invalid budget checkpoint")
	}
	var sum uint64
	for k, n := range s.ByClass {
		switch k {
		case trafficbudget.User, trafficbudget.Control, trafficbudget.Copy, trafficbudget.Config, trafficbudget.APK:
		default:
			return nil, errors.New("invalid budget class")
		}
		if n > s.Used-sum {
			return nil, errors.New("invalid budget totals")
		}
		sum += n
	}
	b, e := NewTrafficBudget(s.Epoch, int64(s.Limit))
	if e != nil {
		return nil, e
	}
	for k, n := range s.ByClass {
		b.ledger.ObserveReceived(n, k)
	}
	b.ledger.ObserveReceived(s.Used-sum, trafficbudget.User)
	if s.Blocked {
		b.meter.Block()
	}
	return b, nil
}
