package research

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestSummarySeparatesExits(t *testing.T) {
	opts := StoreOptions{Dir: t.TempDir(), MaxBytes: 1 << 20}
	s, err := OpenStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	for i, exit := range []string{"home", "work", "direct"} {
		route, typ := "vpn", "quic"
		if exit == "direct" {
			route, typ = "direct", "direct"
		}
		for n := 0; n < 2; n++ {
			data, _ := json.Marshal(map[string]any{"user_id": "u", "route": route, "exit_id": exit, "exit_type": typ, "app": App{Attribution: "known", Packages: []string{"app.one"}}, "bytes_up": "10", "bytes_down": "5", "counter_kind": "delta", "measurement_level": "logical_payload"})
			r := Record{SchemaVersion: 1, Origin: "client", ID: fmt.Sprintf("%d-%d", i, n), Type: "flow_delta", At: time.Now(), Flow: FlowKey{DeviceID: "d", SessionID: exit, FlowID: 1}, Data: data}
			if !s.TryAppend(r) {
				t.Fatal("append")
			}
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result, err := s.ApplicationSummary(context.Background(), Query{UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Partial || len(result.Applications) != 3 {
		t.Fatalf("distinct exits merged: %+v", result)
	}
	for _, a := range result.Applications {
		if a.Flows != 1 || a.BytesUp != 20 || a.BytesDown != 10 {
			t.Fatalf("bad counters: %+v", a)
		}
	}
}

func TestResearchExitCompatibility(t *testing.T) {
	var old FlowData
	if err := json.Unmarshal([]byte(`{"counter_kind":"delta","app":{"attribution":"unknown"}}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.ExitRef.Normalized() != (ExitRef{Route: "unknown", Type: "unknown"}) {
		t.Fatal(old.ExitRef)
	}
	for _, e := range []ExitRef{{Route: "direct", ID: "direct", Type: "direct"}, {Route: "vpn", ID: "home", Type: "quic"}, {}} {
		data, _ := json.Marshal(FlowData{ExitRef: e, CounterKind: "delta", App: App{Attribution: "unknown"}})
		r := Record{SchemaVersion: 1, ID: "x", Type: "flow_delta", At: time.Now(), Data: data}
		if err := validate(r); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range []ExitRef{{Route: "other"}, {Route: "vpn", ID: "direct", Type: "quic"}, {Route: "direct", ID: "home", Type: "direct"}, {Route: "direct", ID: "direct", Type: "https"}} {
		data, _ := json.Marshal(FlowData{ExitRef: e, CounterKind: "delta", App: App{Attribution: "unknown"}})
		for _, kind := range []string{"flow_delta", "dns"} {
			r := Record{SchemaVersion: 1, ID: "x", Type: kind, At: time.Now(), Data: data}
			if validate(r) == nil {
				t.Fatalf("accepted invalid exit %+v for %s", e, kind)
			}
		}
	}
}
func TestResearchExitFilters(t *testing.T) {
	data, _ := json.Marshal(FlowData{ExitRef: ExitRef{Route: "direct", ID: "direct", Type: "direct"}, UserID: "u"})
	r := Record{Type: "flow_delta", At: time.Now(), Data: data}
	if !matches(r, Query{Route: "direct", ExitID: "direct"}) || matches(r, Query{Route: "vpn"}) || matches(r, Query{ExitID: "home"}) {
		t.Fatal("exit filter ignored")
	}
	r.Type = "gap"
	if !matches(r, Query{Route: "vpn", ExitID: "home"}) {
		t.Fatal("session gap lost")
	}
	r.Type = "flow_delta"
	r.Data = []byte(`{}`)
	if !matches(r, Query{Route: "unknown"}) || matches(r, Query{Route: "vpn"}) {
		t.Fatal("legacy classification")
	}
}

func TestSummaryLateExitAttributionKeepsCounters(t *testing.T) {
	opts := StoreOptions{Dir: t.TempDir(), MaxBytes: 1 << 20}
	s, e := OpenStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	for i, exit := range []ExitRef{{Route: "vpn", Type: "unknown"}, {Route: "vpn", ID: "home", Type: "quic"}} {
		data, _ := json.Marshal(FlowData{ExitRef: exit, CounterKind: "delta", BytesUp: 10, MeasurementLevel: "exit_socket_payload", App: App{Attribution: "unknown"}})
		if !s.TryAppend(Record{SchemaVersion: 1, Origin: "server_gateway", Type: "flow_delta", ID: fmt.Sprint(i), At: time.Now(), Flow: FlowKey{SessionID: "s", FlowID: 1}, Data: data}) {
			t.Fatal("append")
		}
	}
	s.Close()
	s, e = OpenStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	summary, e := s.ApplicationSummary(context.Background(), Query{})
	if e != nil {
		t.Fatal(e)
	}
	if summary.Partial || len(summary.Applications) != 1 || summary.Applications[0].BytesUp != 20 || summary.Applications[0].ID != "home" {
		t.Fatalf("late exit lost data: %+v", summary)
	}
}
