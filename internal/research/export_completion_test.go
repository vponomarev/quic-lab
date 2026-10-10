package research

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func seededExportStore(t *testing.T) *Store {
	t.Helper()
	s, e := OpenStore(StoreOptions{Dir: t.TempDir(), MaxBytes: 4000})
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if !s.TryAppend(fixture(id)) {
			t.Fatal("append")
		}
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	return s
}

type terminalSummary struct {
	Type     string `json:"type"`
	Complete bool   `json:"complete"`
	Coverage struct {
		Incomplete bool   `json:"incomplete"`
		Reason     string `json:"reason"`
	} `json:"coverage"`
}

func finalSummary(t *testing.T, b []byte) terminalSummary {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	var v terminalSummary
	if e := json.Unmarshal(lines[len(lines)-1], &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func TestExportHasTerminalSuccess(t *testing.T) {
	s := seededExportStore(t)
	var b bytes.Buffer
	if e := s.Export(context.Background(), Query{}, &b); e != nil {
		t.Fatal(e)
	}
	last := finalSummary(t, b.Bytes())
	if last.Type != "export_summary" || !last.Complete {
		t.Fatalf("no terminal success: %+v", last)
	}
}

type exportHookWriter struct {
	bytes.Buffer
	calls int
	after func(int) error
}

func (w *exportHookWriter) Write(p []byte) (int, error) {
	n, e := w.Buffer.Write(p)
	w.calls++
	if w.after != nil {
		if x := w.after(w.calls); x != nil {
			return n, x
		}
	}
	return n, e
}
func TestExportPrunedSnapshotEndsIncomplete(t *testing.T) {
	s := seededExportStore(t)
	if len(s.segments) < 2 {
		t.Fatal("fixture needs two segments")
	}
	w := &exportHookWriter{after: func(call int) error {
		if call == 2 {
			return os.Remove(s.segments[1].path)
		}
		return nil
	}}
	if e := s.Export(context.Background(), Query{}, w); e == nil {
		t.Fatal("missing snapshot segment silently skipped")
	}
	last := finalSummary(t, w.Bytes())
	if last.Type != "export_summary" || last.Complete || !last.Coverage.Incomplete || last.Coverage.Reason != "history_changed" {
		t.Fatalf("incomplete export not explicit: %+v", last)
	}
}
func TestQueryMissingSnapshotFails(t *testing.T) {
	s := seededExportStore(t)
	if e := os.Remove(s.segments[0].path); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Query(context.Background(), Query{}); e == nil {
		t.Fatal("query silently skipped missing segment")
	}
}
func TestExportWriterFailureNeverReportsSuccess(t *testing.T) {
	s := seededExportStore(t)
	want := errors.New("downstream unavailable")
	w := &exportHookWriter{after: func(call int) error {
		if call >= 2 {
			return want
		}
		return nil
	}}
	if e := s.Export(context.Background(), Query{}, w); !errors.Is(e, want) {
		t.Fatalf("writer error lost: %v", e)
	}
	if strings.Contains(w.String(), `"complete":true`) {
		t.Fatal("failed download marked complete")
	}
}
func TestExportCanceledAfterManifestEndsIncomplete(t *testing.T) {
	s := seededExportStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &exportHookWriter{after: func(call int) error {
		if call == 1 {
			cancel()
		}
		return nil
	}}
	if e := s.Export(ctx, Query{}, w); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancellation lost: %v", e)
	}
	last := finalSummary(t, w.Bytes())
	if last.Type != "export_summary" || last.Complete || last.Coverage.Reason != "canceled" {
		t.Fatalf("cancellation not explicit: %+v", last)
	}
}
func TestGapRetainsOnlyApplicableScopeUnderFlowFilters(t *testing.T) {
	s, e := OpenStore(StoreOptions{Dir: t.TempDir(), MaxBytes: 1 << 20})
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	for _, v := range []struct {
		id, user, device, session string
		at                        time.Time
	}{{"wanted", "alice", "phone", "session", now}, {"other-user", "bob", "phone", "session", now}, {"other-device", "alice", "other", "session", now}, {"other-session", "alice", "phone", "other", now}, {"other-time", "alice", "phone", "session", now.Add(-time.Hour)}} {
		d, _ := json.Marshal(map[string]any{"user_id": v.user, "dropped": "4"})
		if !s.TryAppend(Record{SchemaVersion: 1, Type: "gap", ID: v.id, At: v.at, Generation: 1, Flow: FlowKey{DeviceID: v.device, SessionID: v.session}, Data: d}) {
			t.Fatal("gap append")
		}
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	q := Query{UserID: "alice", DeviceID: "phone", SessionID: "session", From: now.Add(-time.Minute), To: now.Add(time.Minute), Application: "app.one", Protocol: "tcp", Address: "1.2.3.4", Domain: "example.test"}
	p, e := s.Query(context.Background(), q)
	if e != nil || len(p.Records) != 1 || p.Records[0].ID != "wanted" {
		t.Fatalf("applicable gap missing or leaked scope: %+v %v", p, e)
	}
	var b bytes.Buffer
	if e = s.Export(context.Background(), q, &b); e != nil {
		t.Fatal(e)
	}
	dec := json.NewDecoder(&b)
	var gapIDs []string
	for {
		var v struct{ Type, ID string }
		e := dec.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if v.Type == "gap" {
			gapIDs = append(gapIDs, v.ID)
		}
	}
	if len(gapIDs) != 1 || gapIDs[0] != "wanted" {
		t.Fatalf("export gap scope: %v", gapIDs)
	}
}
