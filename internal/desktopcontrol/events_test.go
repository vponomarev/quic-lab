package desktopcontrol

import (
	"encoding/json"
	"strings"
	"testing"
)

const invocationA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func journal(event, invocation, at string) string {
	b, _ := json.Marshal(map[string]string{"MESSAGE": event, "_SYSTEMD_INVOCATION_ID": invocation, "__MONOTONIC_TIMESTAMP": at})
	return string(b)
}

func TestEventsCurrentInvocationAndSanitization(t *testing.T) {
	raw := strings.Join([]string{
		journal(`{"event":"traffic","tx_bytes":9999}`, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ""),
		journal(`{"event":"traffic","tx_bytes":10,"rx_bytes":20}`, invocationA, ""),
		journal(`{"state":"running","readiness":"vpn_dns_ready"}`, invocationA, ""),
		journal(`{"event":"flow_failed","error":"PRIVATE SECRET 1.2.3.4"}`, invocationA, ""),
		journal(`{"event":"readiness","state":"degraded"}`, invocationA, ""),
		journal(`{"event":"traffic","tx_bytes":true,"rx_bytes":9223372036854775808}`, invocationA, ""),
	}, "\n")
	got := publicEvents(raw, invocationA)
	if got.TXBytes != 10 || got.RXBytes != 20 || !got.Started || got.Readiness != "degraded" || got.SessionState != "running" {
		t.Fatalf("bad event status: %+v", got)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "1.2.3.4") {
		t.Fatal("journal data leaked")
	}
	if publicEvents(raw, "").TXBytes != 0 {
		t.Fatal("invalid invocation accepted")
	}
}

func TestProbeFreshnessAndFailureReplaceRTT(t *testing.T) {
	sample := journal(`{"event":"echo","rtt_ms":12.5,"probe_interval_ms":1000,"key":"PRIVATE"}`, invocationA, "100000000")
	for _, tt := range []struct {
		now   float64
		state string
		rtt   bool
	}{{104, "ready", true}, {106, "stale", false}, {99, "unknown", false}} {
		got := publicProbe(sample, invocationA, tt.now)
		if got.TransportState != tt.state || (got.RTTMS != nil) != tt.rtt {
			t.Fatalf("now %v: %+v", tt.now, got)
		}
	}
	for _, tt := range []struct{ event, state string }{{`{"event":"connected"}`, "checking"}, {`{"event":"disconnected"}`, "unavailable"}, {`{"event":"probe_unavailable"}`, "unavailable"}, {`{"event":"echo","rtt_ms":true}`, "unknown"}, {`{"event":"echo","rtt_ms":-1}`, "unknown"}, {`{"event":"echo","rtt_ms":60001}`, "unknown"}, {`{"event":"echo","rtt_ms":1,"probe_interval_ms":999}`, "unknown"}} {
		got := publicProbe(sample+"\n"+journal(tt.event, invocationA, "101000000"), invocationA, 102)
		if got.TransportState != tt.state || got.RTTMS != nil {
			t.Fatalf("%s: %+v", tt.event, got)
		}
	}
	slow := journal(`{"event":"echo","rtt_ms":8,"probe":"https","probe_interval_ms":5000}`, invocationA, "100000000")
	if publicProbe(slow, invocationA, 114).TransportState != "ready" || publicProbe(slow, invocationA, 116).TransportState != "stale" {
		t.Fatal("slow probe freshness incorrect")
	}
	if publicProbe(sample, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 104).RTTMS != nil {
		t.Fatal("mixed invocation RTT")
	}
}

func TestEventsBoundedHistory(t *testing.T) {
	raw := strings.Repeat(journal(`{"state":"running"}`, invocationA, "")+"\n", 150)
	if got := publicEvents(raw, invocationA); len(got.Events) != 20 {
		t.Fatalf("events %d", len(got.Events))
	}
}
