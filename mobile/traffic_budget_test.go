package mobile

import (
	"encoding/json"
	"quiclab/internal/trafficbudget"
	"testing"
)

func TestTrafficBudgetSnapshot(t *testing.T) {
	for _, limit := range []int64{-1} {
		if _, err := NewTrafficBudget("run", limit); err == nil {
			t.Fatal("negative limit")
		}
	}
	if _, err := NewTrafficBudget("", 0); err == nil {
		t.Fatal("missing epoch")
	}
	b, err := NewTrafficBudget("run", 100)
	if err != nil {
		t.Fatal(err)
	}
	a, c := b, b
	id, _ := a.ledger.Reserve(25, trafficbudget.User)
	a.ledger.Commit(id, 20)
	var s trafficbudget.Snapshot
	if err := json.Unmarshal([]byte(c.Snapshot()), &s); err != nil {
		t.Fatal(err)
	}
	if s.Epoch != "run" || s.Used != 20 || s.Limit != 100 || s.Reserved != 0 {
		t.Fatal(s)
	}
}
