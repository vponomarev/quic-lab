package editor

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

var document = json.RawMessage(`{"schema":1,"profiles":[{"id":"default","name":"Test","settings":{}}],"currentProfileId":"default","enabledProfileIds":[],"multiple":false,"globalApps":[],"dns":{"mode":"system","profileId":"default"},"budget":{"limitBytes":0},"diagnostics":{"enabled":true,"detailed":false}}`)

func TestPairRolesAndLifetime(t *testing.T) {
	now := time.Now()
	s := NewStore(func() time.Time { return now })
	ticket, e := s.Create("browser-session", 30*time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Read(ticket.ID, "other", ticket.BrowserToken); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("owner isolation", e)
	}
	grant, e := s.Pair(ticket.ID, ticket.PairToken, 7, document)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pair(ticket.ID, ticket.PairToken, 7, document); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("pair replay", e)
	}
	if _, e = s.Poll(ticket.ID, ticket.BrowserToken); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("browser used device API", e)
	}
	if _, e = s.Read(ticket.ID, "browser-session", grant.DeviceToken); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("device used browser API", e)
	}
	state, e := s.Read(ticket.ID, "browser-session", ticket.BrowserToken)
	if e != nil || state.Generation != 7 {
		t.Fatal(state, e)
	}
	state.Configuration[0] = '!'
	state, _ = s.Read(ticket.ID, "browser-session", ticket.BrowserToken)
	if state.Configuration[0] != '{' {
		t.Fatal("aliased state")
	}
	now = now.Add(30 * time.Minute)
	if _, e = s.Poll(ticket.ID, grant.DeviceToken); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("expired", e)
	}
}
func TestExpiryRevokeAndRestart(t *testing.T) {
	now := time.Now()
	s := NewStore(func() time.Time { return now })
	a, _ := s.Create("owner", 15*time.Minute)
	now = now.Add(5 * time.Minute)
	if _, e := s.Pair(a.ID, a.PairToken, 0, document); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	a, _ = s.Create("owner", 15*time.Minute)
	g, _ := s.Pair(a.ID, a.PairToken, 0, document)
	if e := s.RevokeDevice(a.ID, g.DeviceToken); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Read(a.ID, "owner", a.BrowserToken); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	a, _ = s.Create("owner", 15*time.Minute)
	if _, e := NewStore(time.Now).Read(a.ID, "owner", a.BrowserToken); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
}
func TestSubmitCASAndAcknowledgement(t *testing.T) {
	s := NewStore(time.Now)
	a, _ := s.Create("owner", 30*time.Minute)
	g, _ := s.Pair(a.ID, a.PairToken, 3, document)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Submit(a.ID, "owner", a.BrowserToken, 3, document); results <- e }()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, ErrConflict) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal(success)
	}
	state, _ := s.Poll(a.ID, g.DeviceToken)
	if state.Pending == nil || state.Status != "pending" {
		t.Fatal(state)
	}
	if e := s.Acknowledge(a.ID, g.DeviceToken, state.Pending.Revision+1, 4, document, "applied"); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if e := s.Acknowledge(a.ID, g.DeviceToken, state.Pending.Revision, 4, document, "applied"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Submit(a.ID, "owner", a.BrowserToken, 3, document); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if _, e := s.Submit(a.ID, "owner", a.BrowserToken, 4, document); e != nil {
		t.Fatal(e)
	}
}
func TestLimitsAndInvalidDocuments(t *testing.T) {
	s := NewStore(time.Now)
	if _, e := s.Create("owner", time.Hour*2); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	a, _ := s.Create("owner", time.Hour)
	if _, e := s.Pair(a.ID, a.PairToken, 0, json.RawMessage(`{}`)); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if _, e := s.Pair(a.ID, a.PairToken, 0, document); e != nil {
		t.Fatal("invalid request consumed token", e)
	}
	for i := 1; i < 100; i++ {
		if _, e := s.Create("owner", time.Hour); e != nil {
			t.Fatal(i, e)
		}
	}
	if _, e := s.Create("owner", time.Hour); !errors.Is(e, ErrLimit) {
		t.Fatal(e)
	}
}

func TestAcknowledgementRetryAndLocalEdit(t *testing.T) {
	s := NewStore(time.Now)
	a, _ := s.Create("owner", 30*time.Minute)
	g, _ := s.Pair(a.ID, a.PairToken, 1, document)
	revision, _ := s.Submit(a.ID, "owner", a.BrowserToken, 1, document)
	if e := s.Acknowledge(a.ID, g.DeviceToken, revision, 2, document, "applied"); e != nil {
		t.Fatal(e)
	}
	newer, _ := s.Submit(a.ID, "owner", a.BrowserToken, 2, document)
	if e := s.Acknowledge(a.ID, g.DeviceToken, revision, 2, document, "applied"); e != nil {
		t.Fatal("lost response retry", e)
	}
	state, _ := s.Poll(a.ID, g.DeviceToken)
	if state.Pending == nil || state.Pending.Revision != newer {
		t.Fatal("retry erased new candidate")
	}
	if e := s.Report(a.ID, g.DeviceToken, 3, document); e != nil {
		t.Fatal(e)
	}
	state, _ = s.Poll(a.ID, g.DeviceToken)
	if state.Pending != nil {
		t.Fatal("local edit retained stale candidate")
	}
	if e := s.Acknowledge(a.ID, g.DeviceToken, newer, 4, document, "applied"); !errors.Is(e, ErrConflict) {
		t.Fatal("stale apply acknowledged", e)
	}
}
