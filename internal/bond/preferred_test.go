package bond

import (
	"context"
	"testing"
	"time"
)

func TestRuntimePreferenceSurvivesFasterCandidate(t *testing.T) {
	s := New(context.Background(), func(Record) bool { return true })
	defer s.Close()
	a, _, _ := pair(time.Millisecond)
	b, _, _ := pair(time.Millisecond)
	if err := s.AddNamedPath(PathInfo{ID: "primary", ProfileID: "preferred", Network: "wifi", Generation: 1}, a); err != nil {
		t.Fatal(err)
	}
	if err := s.AddNamedPath(PathInfo{ID: "candidate", ProfileID: "candidate", Network: "wifi", Generation: 1}, b); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.paths["primary"].rtt = 100 * time.Millisecond
	s.paths["candidate"].rtt = time.Millisecond
	s.mu.Unlock()
	s.PreferPath("primary")
	s.mu.Lock()
	defer s.mu.Unlock()
	if got := s.preferred(time.Now()); got != "primary" {
		t.Fatalf("profile preference lost to unpromoted candidate: %s", got)
	}
	s.paths["primary"].penalizedUntil = time.Now().Add(time.Second)
	if got := s.preferred(time.Now()); got != "candidate" {
		t.Fatalf("penalized preferred path blocked rescue: %s", got)
	}
}
