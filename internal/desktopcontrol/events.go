package desktopcontrol

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

const maxOutput = 1 << 20

func emptyEvents() Status {
	return Status{SessionState: "unknown", Readiness: "unknown", Events: []string{}}
}
func emptyProbe() Status {
	return Status{TransportState: "unknown", ProbeIntervalMS: 1000, Probe: "echo"}
}

// Only the most recent bounded set of records from this invocation is parsed.
func journalEvents(raw, invocation string, visit func(map[string]json.RawMessage, map[string]json.RawMessage)) {
	if !validInvocation(invocation) || len(raw) > maxOutput {
		return
	}
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	if len(lines) > 100 {
		lines = lines[len(lines)-100:]
	}
	for _, line := range lines {
		var entry, event map[string]json.RawMessage
		if json.Unmarshal([]byte(line), &entry) != nil || jsonString(entry["_SYSTEMD_INVOCATION_ID"]) != invocation {
			continue
		}
		message := jsonString(entry["MESSAGE"])
		if json.Unmarshal([]byte(message), &event) != nil || event == nil {
			continue
		}
		visit(entry, event)
	}
}

func jsonString(raw json.RawMessage) string { var s string; _ = json.Unmarshal(raw, &s); return s }

func publicEvents(raw, invocation string) Status {
	result := emptyEvents()
	journalEvents(raw, invocation, func(_ map[string]json.RawMessage, event map[string]json.RawMessage) {
		kind := jsonString(event["event"])
		if kind == "traffic" {
			for key, target := range map[string]*uint64{"tx_bytes": &result.TXBytes, "rx_bytes": &result.RXBytes} {
				n, err := strconv.ParseUint(string(event[key]), 10, 63)
				if err == nil {
					*target = n
				}
			}
		}
		state := jsonString(event["state"])
		noKind := len(event["event"]) == 0 || string(event["event"]) == "null"
		if (noKind || kind == "network") && oneOf(state, "running", "stopped", "ready", "reconnecting") {
			result.SessionState = state
			result.Started = state == "running" || state == "ready"
			result.Events = append(result.Events, state)
		}
		ready := jsonString(event["readiness"])
		if kind == "readiness" {
			ready = state
		}
		if oneOf(ready, "engine_ready", "vpn_dns_ready", "checking", "reconnecting", "unavailable", "degraded") {
			result.Readiness = ready
			result.Events = append(result.Events, ready)
		}
		if kind == "flow_failed" {
			result.Events = append(result.Events, "flow_failed")
		}
		if s := jsonString(event["status"]); kind == "reload" && oneOf(s, "applied", "rejected") {
			result.Events = append(result.Events, "reload_"+s)
		}
	})
	if len(result.Events) > 20 {
		result.Events = result.Events[len(result.Events)-20:]
	}
	return result
}

func publicProbe(raw, invocation string, now float64) Status {
	result := emptyProbe()
	journalEvents(raw, invocation, func(entry, event map[string]json.RawMessage) {
		kind := jsonString(event["event"])
		if !oneOf(kind, "echo", "health", "connected", "disconnected", "probe_unavailable") {
			return
		}
		// Even an invalid new diagnostic invalidates the previous successful RTT.
		result = emptyProbe()
		micros, err := strconv.ParseUint(jsonString(entry["__MONOTONIC_TIMESTAMP"]), 10, 64)
		at := float64(micros) / 1e6
		if err != nil || at > now || math.IsNaN(now) || math.IsInf(now, 0) {
			return
		}
		interval := 1000
		if value, ok := event["probe_interval_ms"]; ok {
			n, e := strconv.Atoi(string(value))
			if e != nil || (n != 1000 && n != 5000) {
				return
			}
			interval = n
		}
		var rtt *float64
		if kind == "echo" {
			n, e := strconv.ParseFloat(string(event["rtt_ms"]), 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 60000 {
				return
			}
			rtt = &n
		}
		state := "ready"
		if kind == "connected" {
			state = "checking"
		}
		if kind == "disconnected" || kind == "probe_unavailable" {
			state = "unavailable"
		}
		if (state == "ready" || state == "checking") && now-at > math.Max(float64(interval)*3/1000, 5) {
			state = "stale"
			rtt = nil
		}
		probe := jsonString(event["probe"])
		if !oneOf(probe, "echo", "icmp", "https") {
			probe = "echo"
		}
		result = Status{TransportState: state, RTTMS: rtt, ProbeAt: &at, ProbeIntervalMS: interval, Probe: probe}
	})
	return result
}
