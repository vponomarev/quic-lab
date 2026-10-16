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

func TestPreferredSameNetworkAvoidsSilentFastPath(t *testing.T) {
	for _, network := range []string{"wifi", "cell"} {
		for _, stale := range []bool{false, true} {
			now := time.Now()
			bad := &pathState{info: PathInfo{Network: network}, rtt: time.Millisecond, lastReply: now, healthySince: now.Add(-time.Minute)}
			if stale {
				bad.lastReply = now.Add(-3 * time.Second)
			} else {
				bad.penalizedUntil = now.Add(time.Second)
			}
			s := &Session{preferredPath: "primary", paths: map[string]*pathState{"primary": bad, "reserve": {info: PathInfo{Network: network}, rtt: 20 * time.Millisecond, lastReply: now, healthySince: now.Add(-time.Minute)}}}
			if got := s.preferred(now); got != "reserve" {
				t.Fatalf("network=%s stale=%v selected %s", network, stale, got)
			}
		}
	}
}
