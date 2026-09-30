package mobile

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestEchoCadenceUnaffected(t *testing.T) {
	for _, p := range []*probePolicy{nil, {}} {
		on, d, _ := p.snapshot(50 * time.Millisecond)
		if !on || d != 50*time.Millisecond {
			t.Fatal("Echo defaults changed")
		}
		fields := map[string]any{"rtt_ms": 12.0}
		if kind, ok := p.event("echo", fields); !ok || kind != "echo" || fields["rtt_ms"] != 12.0 {
			t.Fatal("Echo hidden")
		}
	}
}
func TestVPNDiagnosticsDisabledKeepsHealth(t *testing.T) {
	g := NewGateway(nil)
	g.SetRTT(false, 1000)
	on, d, _ := g.probes.snapshot(100 * time.Millisecond)
	if on || d != 5*time.Second {
		t.Fatal("health cadence")
	}
	fields := map[string]any{"rtt_ms": 12.0, "gap_ms": 5000.0, "connection_id": "same"}
	kind, ok := g.probes.event("echo", fields)
	if !ok || kind != "health" || fields["rtt_ms"] != nil || fields["gap_ms"] != nil || fields["connection_id"] != "same" {
		t.Fatal("health exposes metrics or loses identity")
	}
	if _, ok := g.probes.event("transit_echo", nil); ok {
		t.Fatal("transit RTT published when disabled")
	}
	if _, ok := g.probes.event("disconnected", nil); !ok {
		t.Fatal("recovery event hidden")
	}
	g.SetRTT(true, 5000)
	if on, d, _ := g.probes.snapshot(time.Second); !on || d != 5*time.Second {
		t.Fatal("enabled cadence")
	}
}
func TestProbeScheduleRespondsToPolicyAndCancellation(t *testing.T) {
	p := &probePolicy{}
	p.set(false, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- p.wait(ctx, time.Second) }()
	p.set(true, time.Second)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("unexpected cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("screen-on did not reschedule pending health timer")
	}
	go func() { done <- p.wait(ctx, time.Second) }()
	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("sent after stop")
		}
	case <-time.After(time.Second):
		t.Fatal("stop blocked")
	}
}
func TestDisabledTransitDoesNotOpenConnection(t *testing.T) {
	g := NewGateway(nil)
	g.SetRTT(false, 1000)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); g.transitHeartbeat(ctx, nil, "unused") }()
	// g.open would fail/panic for an unstarted gateway; a disabled probe must not call it.
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disabled transit did not stop")
	}
}

type policySink struct{ events chan map[string]any }

func (s *policySink) OnEvent(raw string) {
	var e map[string]any
	json.Unmarshal([]byte(raw), &e)
	s.events <- e
}
func TestQUICAndGatewayEventsApplySamePolicy(t *testing.T) {
	sink := &policySink{make(chan map[string]any, 4)}
	g := NewGateway(sink)
	g.SetRTT(false, 1000)
	c := NewClient(sink)
	c.probes = &g.probes
	c.emit("echo", map[string]any{"rtt_ms": 1})
	g.emit("echo", map[string]any{"rtt_ms": 2})
	for i := 0; i < 2; i++ {
		e := <-sink.events
		if e["event"] != "health" || e["rtt_ms"] != nil {
			t.Fatal(e)
		}
	}
	g.SetRTT(true, 1000)
	c.emit("echo", map[string]any{"rtt_ms": 3})
	if e := <-sink.events; e["event"] != "echo" {
		t.Fatal(e)
	}
}
