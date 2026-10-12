package research

import (
	"context"
	"encoding/json"
	"fmt"
	"quiclab/internal/netobserve"
	"strings"
	"testing"
	"time"
)

func TestConnectionsLateMetadataAndSourceIsolation(t *testing.T) {
	opts := StoreOptions{Dir: t.TempDir(), MaxBytes: 1 << 20}
	s, e := OpenStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	seq := 0
	put := func(kind, origin string, data any) {
		seq++
		raw, _ := json.Marshal(data)
		if !s.TryAppend(Record{SchemaVersion: 1, ID: time.Unix(int64(seq), 0).String(), Type: kind, Origin: origin, At: now.Add(time.Duration(seq) * time.Second), Flow: FlowKey{DeviceID: "d", SessionID: "s", FlowID: 1}, Data: raw}) {
			t.Fatal("append")
		}
	}
	for _, origin := range []string{"client", "server_gateway"} {
		d := FlowData{UserID: "u", App: App{Attribution: "unknown"}, CounterKind: "delta", Protocol: "tcp", BytesUp: 4, ObservedFrom: now, ObservedTo: now}
		put("flow_open", origin, d)
		d.BytesUp = 7
		d.TLS = &netobserve.TLSInfo{State: "observed", ServerName: "outer.example", ECHOffered: true}
		put("flow_close", origin, d)
		put("app", origin, map[string]any{"user_id": "u", "attribution": "known", "packages": []string{"app.one"}})
	}
	put("gap", "client", map[string]string{"user_id": "u", "dropped": "41", "reason": "client_queue_or_delivery_loss"})
	s.Close()
	s, e = OpenStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	result, e := s.Connections(context.Background(), Query{UserID: "u", Application: "app.one", SNI: "<ECH>", Limit: 1})
	if e != nil || len(result.Rows) != 1 || result.NextCursor == "" || result.Coverage.ClientDropped != 41 {
		t.Fatalf("%+v %v", result, e)
	}
	row := result.Rows[0]
	if row.Flow.BytesUp != 11 || row.Flow.Status != "closed" || row.Flow.TLS.Label() != "<ECH>" {
		t.Fatalf("%+v", row)
	}
	next, e := s.Connections(context.Background(), Query{UserID: "u", Application: "app.one", SNI: "<ECH>", Limit: 1, Cursor: result.NextCursor})
	if e != nil || len(next.Rows) != 1 || next.Rows[0].Record.Origin == row.Record.Origin {
		t.Fatalf("source pagination %+v %v", next, e)
	}
	none, e := s.Connections(context.Background(), Query{UserID: "foreign"})
	if e != nil || len(none.Rows) != 0 || none.Coverage.ClientDropped != 0 {
		t.Fatal("identity leak", none, e)
	}
}

func TestConnectionsBudgetIncludesAppAndExistingFlows(t *testing.T) {
	for _, kind := range []string{"app", "flow_delta"} {
		t.Run(kind, func(t *testing.T) {
			opts := StoreOptions{Dir: t.TempDir(), MaxBytes: 128 << 20, QueueSize: 1024}
			s, e := OpenStore(opts)
			if e != nil {
				t.Fatal(e)
			}
			app := App{Attribution: "known"}
			for i := 0; i < 32; i++ {
				app.Packages = append(app.Packages, strings.Repeat("p", 950))
				app.Labels = append(app.Labels, strings.Repeat("l", 950))
			}
			var data any = FlowData{UserID: "u", App: app, CounterKind: "delta", BytesUp: 1}
			if kind == "app" {
				data = struct {
					App
					UserID string `json:"user_id"`
				}{app, "u"}
			}
			raw, _ := json.Marshal(data)
			for i := 0; i < 600; i++ {
				flow := uint64(1)
				if kind == "app" {
					flow = uint64(i + 1)
				}
				if !s.TryAppend(Record{SchemaVersion: 1, ID: fmt.Sprint(i), Type: kind, Origin: "client", At: time.Now(), Flow: FlowKey{DeviceID: "d", SessionID: "s", FlowID: flow}, Data: raw}) {
					t.Fatal("append")
				}
			}
			s.Close()
			s, e = OpenStore(opts)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			p, e := s.Connections(context.Background(), Query{UserID: "u"})
			if e != nil || !p.Partial || !p.Coverage.Incomplete {
				t.Fatal("budget bypass", p.Partial, e)
			}
		})
	}
}
