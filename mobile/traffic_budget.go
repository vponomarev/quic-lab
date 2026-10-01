package mobile

import (
	"encoding/json"
	"errors"
	"quiclab/internal/trafficbudget"
)

// TrafficBudget is owned by the whole VPN run, never by an individual Gateway.
// Every cellular transport in this run shares this meter, including draining paths.
type TrafficBudget struct {
	ledger *trafficbudget.Ledger
	meter  *trafficbudget.Meter
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
