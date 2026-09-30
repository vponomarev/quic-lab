package bond

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestRetentionPreservesPendingUntilGrace(t *testing.T) {
	s := NewWithOptions(context.Background(), func(Record) bool { return true }, Options{})
	defer s.Close()
	s.mu.Lock()
	s.recordTimeout = 20 * time.Millisecond
	s.mu.Unlock()
	if e := s.Send(context.Background(), Record{Kind: Data, Flow: 1, Payload: []byte("keep")}); e != nil {
		t.Fatal(e)
	}
	s.mu.Lock()
	s.emptySince = time.Now().Add(-119 * time.Second)
	s.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	if s.Context().Err() != nil || s.Stats().Pending != 1 {
		t.Fatal("disconnected session or reliable data expired before grace")
	}
	s.mu.Lock()
	s.emptySince = time.Now().Add(-120 * time.Second)
	s.mu.Unlock()
	select {
	case <-s.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("grace did not expire")
	}
}
func TestPathGenerationReplacement(t *testing.T) {
	s := New(context.Background(), func(Record) bool { return true })
	defer s.Close()
	a, _, _ := pair(0)
	b, _, _ := pair(0)
	late, _, _ := pair(0)
	defer late.Close()
	info := PathInfo{ID: "profile-1-path-2", ProfileID: "profile-1", Network: "wifi", Generation: 1}
	if e := s.AddNamedPath(info, a); e != nil {
		t.Fatal(e)
	}
	info.Generation = 2
	if e := s.AddNamedPath(info, b); e != nil {
		t.Fatal(e)
	}
	info.Generation = 1
	if s.AddNamedPath(info, late) == nil {
		t.Fatal("stale generation replaced path")
	}
	s.RemoveNamedPath(info)
	time.Sleep(20 * time.Millisecond)
	if !s.HasPath(info.ID) {
		t.Fatal("old receiver removed replacement")
	}
}
func TestFlowIDExhaustionDrains(t *testing.T) {
	m := NewMux(context.Background(), true)
	defer m.Close()
	f, e := m.OpenStream(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	m.mu.Lock()
	m.next = 65536
	m.mu.Unlock()
	if _, e = m.OpenStream(context.Background()); e != ErrDraining {
		t.Fatalf("want draining, got %v", e)
	}
	if m.Context().Err() != nil || f.writeCtx.Err() != nil {
		t.Fatal("exhaustion killed existing flow")
	}
	m.Close()
	server := NewMux(context.Background(), false)
	server.Close()
	if server.deliver(Record{Kind: Open, Flow: 123}) {
		t.Fatal("closed session accepted late OPEN")
	}
}
func TestPendingLimits(t *testing.T) {
	s := NewWithOptions(context.Background(), func(Record) bool { return true }, Options{MaxPendingSession: 3, MaxPendingFlow: 2})
	defer s.Close()
	for _, id := range []uint32{1, 1, 2} {
		if e := s.Send(context.Background(), Record{Kind: Data, Flow: id}); e != nil {
			t.Fatal(e)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if s.Send(ctx, Record{Kind: Data, Flow: 3}) == nil {
		t.Fatal("session queue exceeded")
	}
	if s.Stats().Pending != 3 {
		t.Fatal("queue size changed")
	}
}
func TestNamedPathCarriesDataAfterDisconnect(t *testing.T) {
	a := NewMux(context.Background(), true)
	b := NewMux(context.Background(), false)
	defer a.Close()
	defer b.Close()
	a.Session.mu.Lock()
	a.Session.recordTimeout = 2 * time.Second
	a.Session.mu.Unlock()
	f, e := a.OpenStream(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Write([]byte("retained")); e != nil {
		t.Fatal(e)
	}
	time.Sleep(2100 * time.Millisecond)
	ap, bp, _ := pair(time.Millisecond)
	info := PathInfo{ID: "reserve-slot-2", ProfileID: "backup", Network: "cell", Generation: 1}
	if e = a.Session.AddNamedPath(info, ap); e != nil {
		t.Fatal(e)
	}
	if e = b.Session.AddNamedPath(info, bp); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	remote, e := b.AcceptStream(ctx)
	if e != nil {
		t.Fatal(e)
	}
	remote.SetDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 8)
	if _, e = io.ReadFull(remote, buf); e != nil || string(buf) != "retained" {
		t.Fatalf("payload %q: %v", buf, e)
	}
	if len(a.Session.Stats().Paths) != 1 {
		t.Fatal("named path absent from stats")
	}
}
