package linuxclient

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func awaitHealth(t *testing.T, h *healthMonitor, state string, failures int) {
	t.Helper()
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		s := h.snapshot()
		if s.State == state && s.Failures == failures {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("health state", h.snapshot())
}
func TestHealthMonitorThresholdResetAndStop(t *testing.T) {
	results := make(chan error)
	var calls atomic.Int32
	h := newHealthMonitor(context.Background(), func(ctx context.Context) error {
		calls.Add(1)
		select {
		case e := <-results:
			return e
		case <-ctx.Done():
			return ctx.Err()
		}
	}, time.Millisecond, time.Second, false)
	defer h.stop()
	results <- nil
	awaitHealth(t, h, "vpn_dns_ready", 0)
	results <- errors.New("failed")
	awaitHealth(t, h, "degraded", 1)
	results <- nil
	awaitHealth(t, h, "vpn_dns_ready", 0)
	for i := 1; i <= 3; i++ {
		results <- errors.New("failed")
		state := "degraded"
		if i == 3 {
			state = "unavailable"
		}
		awaitHealth(t, h, state, i)
	}
	before := calls.Load()
	time.Sleep(15 * time.Millisecond)
	if calls.Load() != before {
		t.Fatal("unavailable generation restarted probing")
	}
	h = newHealthMonitor(context.Background(), func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }, time.Hour, time.Hour, false)
	done := make(chan struct{})
	go func() { h.stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked")
	}
}
func TestHealthDNSConfigurationScope(t *testing.T) {
	c := Config{Transport: "vless", Endpoint: "198.18.0.2:443", Targets: []string{"192.0.2.0/24"}, VLESSURI: testVLESS, HealthDNS: "192.0.2.2"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"1.1.1.1", "127.0.0.1", "::1", "dns.example", "192.0.2.2:53"} {
		bad := c
		bad.HealthDNS = target
		if bad.Validate() == nil {
			t.Fatal("out-of-scope health DNS", target)
		}
	}
	c.Transport = "quic"
	c.VLESSURI = ""
	if c.Validate() == nil {
		t.Fatal("unsupported health transport")
	}
}

func TestHealthRetirementRejectsLateSuccess(t *testing.T) {
	started := make(chan struct{})
	h := newHealthMonitor(context.Background(), func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return nil // A retiring generation must not publish a late success.
	}, time.Hour, time.Hour, false)
	<-started
	h.retire()
	if h.snapshot().State != "unavailable" {
		t.Fatal(h.snapshot())
	}
	select {
	case <-h.done:
	default:
		t.Fatal("retired worker still running")
	}
	var calls atomic.Int32
	h = newHealthMonitor(context.Background(), func(context.Context) error { calls.Add(1); return nil }, time.Hour, time.Second, true)
	defer h.stop()
	if h.snapshot().State != "vpn_dns_ready" || calls.Load() != 0 {
		t.Fatal("verified replacement immediately reprobed")
	}
}
