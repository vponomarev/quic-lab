package linuxclient

import (
	"context"
	"sync"
	"time"
)

const healthInterval = 5 * time.Second
const healthTimeout = 2 * time.Second

// HealthStatus describes only the configured VPN DNS probe, not all traffic.
type HealthStatus struct {
	State    string `json:"state"`
	Failures int    `json:"consecutive_failures"`
}

type healthMonitor struct {
	mu     sync.Mutex
	status HealthStatus
	cancel context.CancelFunc
	done   chan struct{}
}

func newHealthMonitor(parent context.Context, probe func(context.Context) error, interval, timeout time.Duration, verified bool) *healthMonitor {
	ctx, cancel := context.WithCancel(parent)
	h := &healthMonitor{cancel: cancel, done: make(chan struct{}), status: HealthStatus{State: "checking"}}
	if verified {
		h.status.State = "vpn_dns_ready"
	}
	go func() {
		defer close(h.done)
		wait := verified
		for {
			if wait {
				timer := time.NewTimer(interval)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			if ctx.Err() != nil {
				return
			}
			attempt, end := context.WithTimeout(ctx, timeout)
			err := probe(attempt)
			end()
			if ctx.Err() != nil {
				return
			}
			h.mu.Lock()
			if err == nil {
				h.status = HealthStatus{State: "vpn_dns_ready"}
			} else {
				h.status.Failures++
				h.status.State = "degraded"
				if h.status.Failures >= 3 {
					h.status.State = "unavailable"
				}
			}
			failed := h.status.State == "unavailable"
			h.mu.Unlock()
			if failed {
				return
			}
			wait = true
		}
	}()
	return h
}

func (h *healthMonitor) snapshot() HealthStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status
}

func (h *healthMonitor) stop() { h.cancel(); <-h.done }

// Retire joins the old worker before a replacement generation can publish ready.
func (h *healthMonitor) retire() {
	h.stop()
	h.mu.Lock()
	h.status.State = "unavailable"
	h.mu.Unlock()
}
