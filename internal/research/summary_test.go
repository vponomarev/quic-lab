package research

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestApplicationSummaryUniqueFlowsDNSAndSources(t *testing.T) {
	opts := StoreOptions{Dir: t.TempDir(), MaxBytes: 1 << 20}
	s, e := OpenStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	appendRow := func(id, kind, origin, user string, data map[string]any) {
		data["user_id"] = user
		raw, _ := json.Marshal(data)
		if !s.TryAppend(Record{SchemaVersion: 1, ID: id, Type: kind, Origin: origin, At: time.Now(), Flow: FlowKey{DeviceID: "d", SessionID: "s", FlowID: 1}, Data: raw}) {
			t.Fatal("append")
		}
	}
	for _, origin := range []string{"client", "server_gateway"} {
		for _, kind := range []string{"flow_open", "flow_delta", "flow_close"} {
			appendRow(origin+kind, kind, origin, "u", map[string]any{"bytes_up": "4", "bytes_down": "2", "counter_kind": "delta", "measurement_level": "payload", "app": App{Attribution: "unknown"}})
		}
		appendRow(origin+"app", "app", origin, "u", map[string]any{"packages": []string{"app.one"}, "labels": []string{"One"}, "attribution": "known"})
		appendRow(origin+"query", "dns", origin, "u", map[string]any{"response": false})
		appendRow(origin+"response", "dns", origin, "u", map[string]any{"response": true})
	}
	appendRow("foreign", "flow_delta", "client", "foreign", map[string]any{"bytes_up": "999", "counter_kind": "delta", "app": App{Attribution: "unknown"}})
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = OpenStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	result, e := s.ApplicationSummary(context.Background(), Query{UserID: "u", Limit: 1, Cursor: "ignored"})
	if e != nil {
		t.Fatal(e)
	}
	if result.Partial || len(result.Applications) != 2 {
		t.Fatalf("%+v", result)
	}
	for _, a := range result.Applications {
		if a.Flows != 1 || a.DNSQueries != 1 || a.BytesUp != 12 || a.BytesDown != 6 || len(a.App.Packages) != 1 || a.App.Packages[0] != "app.one" {
			t.Fatalf("%+v", a)
		}
	}
	only, e := s.ApplicationSummary(context.Background(), Query{UserID: "u", Origin: "client"})
	if e != nil || len(only.Applications) != 1 || only.Applications[0].Origin != "client" {
		t.Fatal(only, e)
	}
}
