//go:build linux && !android

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestTransportDiagnosticsAreSanitized(t *testing.T) {
	for _, transport := range []string{"quic", "https", "awg", "vless"} {
		t.Run(transport, func(t *testing.T) {
			var b bytes.Buffer
			s := &sink{writer: &b, transport: transport}
			for _, event := range []string{"connected", "echo", "health", "probe_unavailable", "disconnected"} {
				s.OnEvent(`{"event":"` + event + `","rtt_ms":12.5,"connection_id":"PRIVATE","peer":"PRIVATE","detail":"PRIVATE"}`)
			}
			lines := strings.Split(strings.TrimSpace(b.String()), "\n")
			if len(lines) != 5 || strings.Contains(b.String(), "PRIVATE") {
				t.Fatal(b.String())
			}
			for i, line := range lines {
				var v map[string]any
				if err := json.Unmarshal([]byte(line), &v); err != nil {
					t.Fatal(err)
				}
				interval := float64(1000)
				if transport == "vless" {
					interval = 5000
				}
				if v["probe_interval_ms"] != interval {
					t.Fatal(v)
				}
				if i == 1 {
					if v["rtt_ms"] != 12.5 {
						t.Fatal(v)
					}
				} else if _, ok := v["rtt_ms"]; ok {
					t.Fatal("non-echo has RTT", v)
				}
			}
		})
	}
}

func TestInvalidRTTIsNotAHealthyReply(t *testing.T) {
	var b bytes.Buffer
	s := &sink{writer: &b}
	for _, value := range []string{`-1`, `"12"`, `null`, `true`} {
		s.OnEvent(`{"event":"echo","rtt_ms":` + value + `}`)
	}
	s.OnEvent(`{"event":"transit_echo","rtt_ms":1}`)
	if b.Len() != 0 {
		t.Fatal("invalid or unrelated probe forwarded", b.String())
	}
}
