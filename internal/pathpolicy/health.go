// Package pathpolicy makes deterministic transport decisions without dialing sockets.
package pathpolicy

import "time"

// Observation uses cumulative acknowledged payload bytes for one path generation.
// ProbeOK and DataProbeOK are events, not sticky status flags.
type Observation struct {
	PathID                   string
	Generation               uint64
	At                       time.Time
	PendingBytes, AckedBytes uint64
	ProbeOK, DataProbeOK     bool
	DataProbePending         bool
	Connected, Failed        bool
	RTT                      time.Duration
}

type Health struct {
	at, waiting, probeWaiting time.Time
	acked, pending            uint64
	rtt                       time.Duration
}

func (h *Health) Observe(o Observation) {
	if !h.at.IsZero() && o.At.Before(h.at) {
		return
	}
	if o.PendingBytes == 0 {
		h.waiting = time.Time{}
	} else if h.pending == 0 || o.AckedBytes > h.acked {
		h.waiting = o.At
	}
	if !o.DataProbePending {
		h.probeWaiting = time.Time{}
	} else if h.probeWaiting.IsZero() {
		h.probeWaiting = o.At
	}
	h.at, h.pending = o.At, o.PendingBytes
	if o.AckedBytes > h.acked {
		h.acked = o.AckedBytes
	}
	if o.RTT > 0 {
		h.rtt = o.RTT
	}
}
func (h *Health) Stalled(now time.Time) bool {
	threshold := max(300*time.Millisecond, min(3*h.rtt, 800*time.Millisecond))
	return (h.pending > 0 && !h.waiting.IsZero() && now.Sub(h.waiting) >= threshold) || (!h.probeWaiting.IsZero() && now.Sub(h.probeWaiting) >= threshold)
}
