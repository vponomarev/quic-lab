package research

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestDetectedProtocolLateMetadataAndIsolation(t *testing.T) {
	opts := StoreOptions{Dir: t.TempDir(), MaxBytes: 1 << 20}
	s, err := OpenStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, origin := range []string{"client", "server_gateway"} {
		for i := 0; i < 3; i++ {
			d := map[string]any{"user_id": "u", "app": App{Attribution: "known", Packages: []string{"app.one"}}, "protocol": "udp", "destination": "1.1.1.1:443", "counter_kind": "delta", "bytes_up": "4", "measurement_level": "payload"}
			if origin == "server_gateway" && i == 1 {
				d["detected_protocol"] = map[string]string{"name": "QUIC v1", "evidence": "quic_initial"}
				d["tls"] = map[string]any{"state": "observed", "server_name": "example.org", "ech_offered": true}
			}
			raw, _ := json.Marshal(d)
			if !s.TryAppend(Record{SchemaVersion: 1, ID: fmt.Sprintf("%s%d", origin, i), Type: "flow_delta", Origin: origin, At: now.Add(time.Duration(i) * time.Second), Flow: FlowKey{DeviceID: "d", SessionID: "s", FlowID: 1}, Data: raw}) {
				t.Fatal("append")
			}
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, sni := range []string{"", "EXAMPLE.ORG", "<ECH>"} {
		q := Query{UserID: "u", DetectedProtocol: "quic V1", SNI: sni}
		p, err := s.Connections(context.Background(), q)
		if err != nil || len(p.Rows) != 1 {
			t.Fatalf("connections: %+v %v", p, err)
		}
		if p.Rows[0].Record.Origin != "server_gateway" || p.Rows[0].Flow.BytesUp != 12 {
			t.Fatalf("source or delta loss: %+v", p.Rows)
		}
		summary, err := s.ApplicationSummary(context.Background(), q)
		if err != nil || len(summary.Applications) != 1 || summary.Applications[0].Origin != "server_gateway" || summary.Applications[0].BytesUp != 12 {
			t.Fatalf("summary: %+v %v", summary, err)
		}
	}
	p, err := s.Connections(context.Background(), Query{UserID: "u", Origin: "client"})
	if err != nil || len(p.Rows) != 1 || p.Rows[0].Flow.Detected != nil {
		t.Fatalf("legacy protocol inferred: %+v %v", p, err)
	}
}

func TestDetectedProtocolValidation(t *testing.T) {
	for _, name := range []string{"HTTP/3", "<script>", ""} {
		raw, _ := json.Marshal(map[string]any{"app": App{Attribution: "unknown"}, "counter_kind": "delta", "detected_protocol": map[string]string{"name": name, "evidence": "quic_initial"}})
		if err := validate(Record{SchemaVersion: 1, ID: "invalid", At: time.Now(), Type: "flow_delta", Data: raw}); err == nil {
			t.Fatalf("accepted invalid protocol %q", name)
		}
	}
}
