package gateway

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"quiclab/internal/research"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestResearchRevocationReservedAdmission(t *testing.T) {
	s, _ := New("0.0.0.0/0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Research = NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "u", "d", true }, func(research.Record) bool { return true })
	for i := 0; i < cap(s.slots); i++ {
		s.slots <- struct{}{}
	}
	ctx, cancel := context.WithCancel(s.researchContext(context.Background(), tls.ConnectionState{}))
	defer cancel()
	streams := make(chan Stream)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.serve(ctx, nil, "test", func() (Stream, error) {
			select {
			case st := <-streams:
				return st, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}, func() {})
	}()
	a, b := net.Pipe()
	defer a.Close()
	a.SetDeadline(time.Now().Add(2 * time.Second))
	streams <- researchPipe{b}
	if e := WriteJSON(a, Request{Network: "research-v1"}); e != nil {
		t.Fatal(e)
	}
	var reply Reply
	if e := ReadJSON(a, &reply); e != nil {
		t.Fatalf("full data slots rejected control: %v", e)
	}
	if e := WriteJSON(a, ResearchBatch{Session: "0123456789abcdef0123456789abcdef", Generation: 2, Enabled: false}); e != nil {
		t.Fatal(e)
	}
	var result ResearchReply
	if e := ReadJSON(a, &result); e != nil {
		t.Fatal(e)
	}
	if result.Allowed {
		t.Fatal("revoke enabled")
	}
	cancel()
	<-done
}
func TestResearchWireMaximumFitsPacket(t *testing.T) {
	if e := WritePacket(io.Discard, make([]byte, MaxResearchBatchBytes)); e != nil {
		t.Fatalf("advertised research maximum exceeds packet limit: %v", e)
	}
}

func TestVPNAdmissionIndependentOfResearch(t *testing.T) {
	s, _ := New("0.0.0.0/0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := 0; i < cap(s.researchSlots); i++ {
		s.researchSlots <- struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	streams := make(chan Stream)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.serve(ctx, nil, "test", func() (Stream, error) {
			select {
			case st := <-streams:
				return st, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}, func() {})
	}()
	a, b := net.Pipe()
	defer a.Close()
	a.SetDeadline(time.Now().Add(2 * time.Second))
	streams <- researchPipe{b}
	if e := WriteJSON(a, Request{Network: "echo"}); e != nil {
		t.Fatal(e)
	}
	var reply Reply
	if e := ReadJSON(a, &reply); e != nil || reply.Session == "" {
		t.Fatalf("research saturation affected VPN admission: %+v %v", reply, e)
	}
	cancel()
	<-done
}
func TestResearchRechecksPolicyAfterConsentLock(t *testing.T) {
	var enabled atomic.Bool
	enabled.Store(true)
	checked := make(chan struct{})
	var once sync.Once
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) {
		value := enabled.Load()
		once.Do(func() { close(checked) })
		return "u", "d", value
	}, func(research.Record) bool { return true })
	r.mu.Lock()
	done := make(chan ResearchReply, 1)
	go func() {
		done <- r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: "0123456789abcdef0123456789abcdef", Generation: 1, Enabled: true})
	}()
	<-checked
	enabled.Store(false)
	r.mu.Unlock()
	if reply := <-done; reply.Allowed {
		t.Fatal("policy snapshot from before lock was reused")
	}
}
func TestResearchRevocationWithFullSessionFlows(t *testing.T) {
	s, _ := New("0.0.0.0/0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Research = NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "u", "d", true }, func(research.Record) bool { return true })
	ctx, cancel := context.WithCancel(s.researchContext(context.Background(), tls.ConnectionState{}))
	defer cancel()
	streams := make(chan Stream)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.serve(ctx, nil, "test", func() (Stream, error) {
			select {
			case st := <-streams:
				return st, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}, func() {})
	}()
	for i := 0; i < MaxFlows; i++ {
		a, b := net.Pipe()
		defer a.Close()
		a.SetDeadline(time.Now().Add(5 * time.Second))
		streams <- researchPipe{b}
		if e := WriteJSON(a, Request{Network: "echo"}); e != nil {
			t.Fatal(e)
		}
		var reply Reply
		if e := ReadJSON(a, &reply); e != nil {
			t.Fatal(e)
		}
	}
	a, b := net.Pipe()
	defer a.Close()
	a.SetDeadline(time.Now().Add(2 * time.Second))
	streams <- researchPipe{b}
	if e := WriteJSON(a, Request{Network: "research-v1"}); e != nil {
		t.Fatal(e)
	}
	var reply Reply
	if e := ReadJSON(a, &reply); e != nil {
		t.Fatalf("full session blocked revoke: %v", e)
	}
	if e := WriteJSON(a, ResearchBatch{Session: "0123456789abcdef0123456789abcdef", Generation: 2, Enabled: false}); e != nil {
		t.Fatal(e)
	}
	var result ResearchReply
	if e := ReadJSON(a, &result); e != nil {
		t.Fatal(e)
	}
	cancel()
	<-done
}
