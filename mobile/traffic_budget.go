package mobile

import (
	"encoding/json"
	"errors"
	"quiclab/internal/trafficbudget"
)

// TrafficBudget is owned by the whole VPN run, never by an individual Gateway.
// Socket accounting is attached in C2; this wrapper does not meter traffic itself.
type TrafficBudget struct{ ledger *trafficbudget.Ledger }

func NewTrafficBudget(epoch string, limit int64) (*TrafficBudget, error) {
	if epoch == "" || limit < 0 {
		return nil, errors.New("budget requires an epoch and a nonnegative limit")
	}
	return &TrafficBudget{ledger: trafficbudget.New(epoch, uint64(limit))}, nil
}
func (b *TrafficBudget) Snapshot() string {
	raw, _ := json.Marshal(b.ledger.Snapshot())
	return string(raw)
}
