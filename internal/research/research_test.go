package research

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(id string) Record {
	d, _ := json.Marshal(FlowData{UserID: "alice", App: App{ID: math.MaxUint64, UID: 123, Packages: []string{"app.one", "app.two"}, Labels: []string{"Приложение"}, Attribution: "shared_uid"}, Protocol: "tcp", Destination: "1.2.3.4:443", ObservedFrom: time.Now().UTC().Add(-time.Minute), ObservedTo: time.Now().UTC(), BytesUp: math.MaxUint64, CounterKind: "delta", MeasurementLevel: "logical_payload"})
	return Record{SchemaVersion: 1, Type: "flow_delta", ID: id, At: time.Now().UTC(), Generation: 2, Flow: FlowKey{DeviceID: "phone", SessionID: "session", FlowID: math.MaxUint64}, Data: d}
}
func TestRecordRoundTripAndBounds(t *testing.T) {
	r := fixture("one")
	var b bytes.Buffer
	if err := EncodeRecord(&b, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"18446744073709551615"`) {
		t.Fatal("integer precision lost")
	}
	got, err := DecodeRecord(&b)
	if err != nil || got.Flow != r.Flow {
		t.Fatalf("%+v %v", got, err)
	}
	r.SchemaVersion = 2
	if EncodeRecord(&b, r) == nil {
		t.Fatal("unknown version accepted")
	}
	r = fixture("two")
	r.Data = json.RawMessage(`{"junk":"` + strings.Repeat("x", MaxRecordBytes) + `"}`)
	if EncodeRecord(&b, r) == nil {
		t.Fatal("oversize accepted")
	}
	r = fixture("three")
	r.Type = "unexpected"
	if EncodeRecord(&b, r) == nil {
		t.Fatal("unknown type accepted")
	}
}
func TestConsentOrder(t *testing.T) {
	var c ConsentState
	if c.Allows(0) {
		t.Fatal("default consent")
	}
	if !c.Apply(3, false) || c.Apply(2, true) || c.Apply(3, true) || c.Allows(3) {
		t.Fatal("stale consent enabled")
	}
	if !c.Apply(4, true) || !c.Allows(4) || c.Allows(3) || !c.Apply(4, true) {
		t.Fatal("generation mismatch")
	}
}
func TestStoreExportAndRecovery(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(StoreOptions{Dir: dir, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	r := fixture("one")
	if !s.TryAppend(r) || !s.TryAppend(r) {
		t.Fatal("append")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(StoreOptions{Dir: dir, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Query(context.Background(), Query{UserID: "alice", Application: "app.two"})
	if err != nil || len(p.Records) != 1 {
		t.Fatalf("%+v %v", p, err)
	}
	p, _ = s.Query(context.Background(), Query{UserID: "bob"})
	if len(p.Records) != 0 {
		t.Fatal("scope leak")
	}
	var b bytes.Buffer
	if err = s.Export(context.Background(), Query{UserID: "alice"}, &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"type":"manifest"`) || !strings.Contains(b.String(), "Приложение") {
		t.Fatal(b.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.Export(ctx, Query{}, &b) == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestStoreRequiresQuota(t *testing.T) {
	if s, e := OpenStore(StoreOptions{Dir: t.TempDir()}); e == nil {
		s.Close()
		t.Fatal("unbounded store")
	}
}
func TestStoreRecoveryTruncation(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(StoreOptions{Dir: dir, MaxBytes: 1 << 20})
	s.TryAppend(fixture("one"))
	s.Close()
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	f, e := os.OpenFile(files[0], os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteString(`{"unfinished":`)
	f.Close()
	s, e = OpenStore(StoreOptions{Dir: dir, MaxBytes: 1 << 20})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, e := s.Query(context.Background(), Query{})
	if e != nil || len(p.Records) != 1 || p.Coverage.RecoveryGaps == 0 {
		t.Fatalf("%+v %v", p, e)
	}
}
func TestStoreBackpressure(t *testing.T) {
	s, e := OpenStore(StoreOptions{Dir: t.TempDir(), MaxBytes: 1 << 20, QueueSize: 1})
	if e != nil {
		t.Fatal(e)
	}
	s.mu.Lock()
	if !s.TryAppend(fixture("one")) {
		t.Fatal("first append")
	}
	deadline := time.Now().Add(time.Second)
	for len(s.queue) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !s.TryAppend(fixture("two")) {
		s.mu.Unlock()
		t.Fatal("second append")
	}
	if s.TryAppend(fixture("three")) {
		s.mu.Unlock()
		t.Fatal("unbounded queue")
	}
	s.mu.Unlock()
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	p, e := s.Query(context.Background(), Query{})
	if e != nil || len(p.Records) != 2 || p.Coverage.Dropped != 1 {
		t.Fatalf("%+v %v", p, e)
	}
}
func TestRetentionQuotaAndPagination(t *testing.T) {
	dir := t.TempDir()
	s, e := OpenStore(StoreOptions{Dir: dir, MaxBytes: 4000, QueueSize: 32})
	if e != nil {
		t.Fatal(e)
	}
	old := fixture("old")
	old.At = time.Now().Add(-8 * 24 * time.Hour)
	s.TryAppend(old)
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		if !s.TryAppend(fixture(id)) {
			t.Fatal("queue unexpectedly full")
		}
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	var size int64
	for _, f := range files {
		i, _ := os.Stat(f)
		size += i.Size()
	}
	if size > 4000 {
		t.Fatalf("quota exceeded: %d", size)
	}
	p, e := s.Query(context.Background(), Query{Limit: 1})
	if e != nil || len(p.Records) != 1 || p.NextCursor == "" || !p.Coverage.Truncated {
		t.Fatalf("%+v %v", p, e)
	}
	p2, e := s.Query(context.Background(), Query{Limit: 1, Cursor: p.NextCursor})
	if e != nil || len(p2.Records) != 1 || p2.Records[0].ID == p.Records[0].ID {
		t.Fatalf("pagination %+v %v", p2, e)
	}
	for _, r := range append(p.Records, p2.Records...) {
		if r.ID == "old" {
			t.Fatal("expired record returned")
		}
	}
}

type slowExport struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *slowExport) Write(b []byte) (int, error) {
	w.once.Do(func() { close(w.started); <-w.release })
	return len(b), nil
}
func TestSlowExportDoesNotBlockIngest(t *testing.T) {
	s, e := OpenStore(StoreOptions{Dir: t.TempDir(), MaxBytes: 1 << 20})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	w := &slowExport{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- s.Export(context.Background(), Query{}, w) }()
	<-w.started
	if !s.TryAppend(fixture("during-export")) {
		t.Fatal("ingest blocked")
	}
	deadline := time.Now().Add(time.Second)
	for {
		p, e := s.Query(context.Background(), Query{})
		if e != nil {
			t.Fatal(e)
		}
		if len(p.Records) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer blocked by export")
		}
		time.Sleep(time.Millisecond)
	}
	close(w.release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
func TestStoreWriteFailureReported(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "history")
	s, e := OpenStore(StoreOptions{Dir: dir, MaxBytes: 1 << 20})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(dir); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(dir, []byte("not a directory"), 0600); e != nil {
		t.Fatal(e)
	}
	if !s.TryAppend(fixture("one")) {
		t.Fatal("append should enqueue")
	}
	if s.Close() == nil {
		t.Fatal("write failure hidden")
	}
	if s.coverage().Dropped != 1 {
		t.Fatal("write loss hidden")
	}
}
func TestConcurrentCloseAppendQuery(t *testing.T) {
	s, e := OpenStore(StoreOptions{Dir: t.TempDir(), MaxBytes: 1 << 20})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				s.TryAppend(fixture(fmt.Sprintf("%d-%d", i, j)))
				s.Query(context.Background(), Query{Limit: 1})
			}
		}(i)
	}
	wg.Wait()
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if s.TryAppend(fixture("closed")) {
		t.Fatal("accepted after close")
	}
}
