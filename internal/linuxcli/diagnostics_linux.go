//go:build linux && !android

package linuxcli

import "math"

// Publish only the same transport-probe outcomes used by Android. No raw
// errors, addresses, peer IDs or private transport configuration reach the GUI.
func (s *sink) diagnostic(v map[string]any) {
	kind, _ := v["event"].(string)
	switch kind {
	case "echo", "health", "connected", "disconnected", "probe_unavailable":
	default:
		return
	}
	probe, interval := "echo", 1000
	if s.transport == "awg" {
		probe = "icmp"
	}
	if s.transport == "vless" {
		probe, interval = "https", 5000
	}
	out := map[string]any{"event": kind, "probe": probe, "probe_interval_ms": interval}
	if kind == "echo" {
		rtt, ok := v["rtt_ms"].(float64)
		if !ok || math.IsNaN(rtt) || math.IsInf(rtt, 0) || rtt < 0 || rtt > 60000 {
			return
		}
		out["rtt_ms"] = rtt
	}
	s.write(out)
}
