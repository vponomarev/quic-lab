package vpnclient

import (
	"context"
	"errors"
	"testing"
)

type memoryJournal struct {
	r      Journal
	writes int
	failAt int
}

func (s *memoryJournal) Load(context.Context) (Journal, error) {
	b, _ := EncodeJournal(s.r)
	return DecodeJournal(b)
}
func (s *memoryJournal) Save(_ context.Context, r Journal) error {
	s.writes++
	if s.failAt == s.writes {
		return errors.New("disk")
	}
	b, e := EncodeJournal(r)
	if e != nil {
		return e
	}
	s.r, e = DecodeJournal(b)
	return e
}

type fakeNetwork struct {
	values        map[string]Value
	calls, failAt int
	j             *memoryJournal
	t             *testing.T
}

func (n *fakeNetwork) Read(_ context.Context, k string) (Value, error) { return n.values[k], nil }
func (n *fakeNetwork) CompareAndSwap(_ context.Context, k string, from, to Value) error {
	n.calls++
	if n.failAt == n.calls {
		return errors.New("OS failure")
	}
	if n.values[k] != from {
		return ErrConflict
	}
	if n.j.r.Owner == "" {
		n.t.Fatal("mutation before durable intent")
	}
	n.values[k] = to
	return nil
}
func plan() NetworkPlan {
	return NetworkPlan{Owner: "quic-test", Changes: []Change{{Resource: "quic-test/dns", Before: Value{Present: true, Data: "old"}, After: Value{Present: true, Data: "new"}}, {Resource: "quic-test/route", After: Value{Present: true, Data: "tunnel"}}}}
}
func fixture(t *testing.T) (*memoryJournal, *fakeNetwork) {
	s := &memoryJournal{}
	return s, &fakeNetwork{values: map[string]Value{"quic-test/dns": {Present: true, Data: "old"}}, j: s, t: t}
}
func TestPlanRollbackAndIdempotence(t *testing.T) {
	ctx := context.Background()
	s, n := fixture(t)
	p := plan()
	if err := p.Apply(ctx, n, s); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(ctx, n, s); err != nil {
		t.Fatal(err)
	}
	calls := n.calls
	if err := Rollback(ctx, n, s); err != nil || n.calls != calls {
		t.Fatal(err, "rollback repeated")
	}
	if n.values["quic-test/dns"].Data != "old" || n.values["quic-test/route"].Present {
		t.Fatal(n.values)
	}
}
func TestPlanFailureAtEveryStep(t *testing.T) {
	for step := 1; step <= 2; step++ {
		s, n := fixture(t)
		n.failAt = step
		if plan().Apply(context.Background(), n, s) == nil {
			t.Fatal("accepted failure")
		}
		if n.values["quic-test/dns"].Data != "old" || n.values["quic-test/route"].Present {
			t.Fatal(n.values)
		}
	}
}
func TestCrashAfterMutationBeforeResultSave(t *testing.T) {
	s, n := fixture(t)
	p := plan()
	// First entry intent was durably saved; OS mutation happened, process died before next journal write.
	s.r = Journal{Version: 1, Owner: p.Owner, Entries: []JournalEntry{{Change: p.Changes[0], Intent: true}}}
	n.values[p.Changes[0].Resource] = p.Changes[0].After
	if err := Rollback(context.Background(), n, s); err != nil {
		t.Fatal(err)
	}
	if n.values[p.Changes[0].Resource] != p.Changes[0].Before {
		t.Fatal("not restored")
	}
}
func TestRollbackPreservesForeignChange(t *testing.T) {
	s, n := fixture(t)
	p := plan()
	if err := p.Apply(context.Background(), n, s); err != nil {
		t.Fatal(err)
	}
	n.values[p.Changes[0].Resource] = Value{Present: true, Data: "foreign"}
	if !errors.Is(Rollback(context.Background(), n, s), ErrConflict) {
		t.Fatal("missing conflict")
	}
	if n.values[p.Changes[0].Resource].Data != "foreign" {
		t.Fatal("foreign state overwritten")
	}
	if plan().Apply(context.Background(), n, s) == nil {
		t.Fatal("overwrote unresolved journal")
	}
}
func TestJournalFailureBeforeIntentDoesNotMutate(t *testing.T) {
	s, n := fixture(t)
	s.failAt = 2
	if plan().Apply(context.Background(), n, s) == nil {
		t.Fatal("journal failure accepted")
	}
	if n.calls != 0 {
		t.Fatal("unlogged mutation")
	}
}
func TestJournalRejectsForeignResourceAndCorruption(t *testing.T) {
	p := plan()
	p.Changes[0].Resource = "other/dns"
	s, n := fixture(t)
	if p.Apply(context.Background(), n, s) == nil || n.calls != 0 {
		t.Fatal("foreign resource accepted")
	}
	for _, raw := range []string{`{"version":2}`, `{"version":1,"unknown":1}`, `{} {}`} {
		if _, e := DecodeJournal([]byte(raw)); e == nil {
			t.Fatal("accepted", raw)
		}
	}
}
