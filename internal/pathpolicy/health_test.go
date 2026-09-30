package pathpolicy

import (
	"testing"
	"time"
)

func TestNoFalseFailureWhenIdle(t *testing.T) {
	var h Health
	now := time.Unix(100, 0)
	h.Observe(Observation{At: now, ProbeOK: true})
	if h.Stalled(now.Add(time.Hour)) {
		t.Fatal("idle path failed")
	}
}
func TestSmallProbeCannotMaskStall(t *testing.T) {
	var h Health
	now := time.Unix(100, 0)
	h.Observe(Observation{At: now, PendingBytes: 1050, ProbeOK: true})
	h.Observe(Observation{At: now.Add(400 * time.Millisecond), PendingBytes: 1050, ProbeOK: true})
	if !h.Stalled(now.Add(400 * time.Millisecond)) {
		t.Fatal("tiny probe masked blocked payload")
	}
}
func TestVolumeBlackhole(t *testing.T) {
	var h Health
	now := time.Unix(100, 0)
	h.Observe(Observation{At: now, PendingBytes: 1050, AckedBytes: 2 << 20})
	if h.Stalled(now.Add(200 * time.Millisecond)) {
		t.Fatal("premature failure")
	}
	if !h.Stalled(now.Add(time.Second)) {
		t.Fatal("volume blackhole missed")
	}
	h.Observe(Observation{At: now.Add(time.Second), PendingBytes: 1050, AckedBytes: (2 << 20) + 1050})
	if h.Stalled(now.Add(time.Second)) {
		t.Fatal("actual progress ignored")
	}
}
func TestDataProbeCannotMaskPendingStall(t *testing.T) {
	var h Health
	now := time.Unix(100, 0)
	h.Observe(Observation{At: now, PendingBytes: 1050})
	h.Observe(Observation{At: now.Add(time.Second), PendingBytes: 1050, DataProbeOK: true})
	if !h.Stalled(now.Add(time.Second)) {
		t.Fatal("probe masked real pending data")
	}
}
func TestStallThresholdAndStaleObservation(t *testing.T) {
	var h Health
	now := time.Unix(100, 0)
	h.Observe(Observation{At: now, PendingBytes: 1050, RTT: time.Second})
	if h.Stalled(now.Add(799*time.Millisecond)) || !h.Stalled(now.Add(800*time.Millisecond)) {
		t.Fatal("800ms cap")
	}
	h.Observe(Observation{At: now.Add(-time.Second), AckedBytes: 99999})
	if !h.Stalled(now.Add(time.Second)) {
		t.Fatal("stale sample rewound health")
	}
}

func TestSizedProbeStallWhenIdle(t *testing.T) {
	var h Health
	now := time.Unix(100, 0)
	h.Observe(Observation{At: now, DataProbePending: true})
	h.Observe(Observation{At: now.Add(time.Second), DataProbePending: true, ProbeOK: true})
	if !h.Stalled(now.Add(time.Second)) {
		t.Fatal("blocked sized probe ignored")
	}
	h.Observe(Observation{At: now.Add(2 * time.Second), DataProbeOK: true})
	if h.Stalled(now.Add(2 * time.Second)) {
		t.Fatal("completed probe still pending")
	}
}
