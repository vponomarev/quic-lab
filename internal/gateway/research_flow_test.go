package gateway

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"quiclab/internal/research"
	"sync"
	"testing"
	"time"
)

func TestResearchFlowReferenceWire(t *testing.T) {
	ref := ResearchFlowRef{Version: 1, Session: "0123456789abcdef0123456789abcdef", FlowID: 7}
	ctx := WithResearchFlow(context.Background(), ref)
	a, b := net.Pipe()
	defer b.Close()
	got := make(chan Request, 1)
	go func() { var req Request; _ = ReadJSON(b, &req); got <- req; _ = WriteJSON(b, Reply{}) }()
	st, e := Open(ctx, func(context.Context) (Stream, error) { return researchPipe{a}, nil }, "tcp", "1.2.3.4:443")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	req := <-got
	if req.Research == nil || *req.Research != ref {
		t.Fatalf("missing association: %+v", req)
	}
	raw, _ := json.Marshal(req)
	var legacy struct{ Network, Address string }
	if json.Unmarshal(raw, &legacy) != nil || legacy.Network != "tcp" {
		t.Fatal("legacy request incompatible")
	}
}
func TestServerResearchConsentAndCounters(t *testing.T) {
	var mu sync.Mutex
	var records []research.Record
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "user", "device", true }, func(v research.Record) bool { mu.Lock(); records = append(records, v); mu.Unlock(); return true })
	ref := &ResearchFlowRef{Version: 1, Session: "0123456789abcdef0123456789abcdef", FlowID: 7}
	r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 1, Enabled: false})
	a, b := net.Pipe()
	defer b.Close()
	ctx := context.WithValue(context.Background(), researchContextKey{}, tls.ConnectionState{})
	s := &Server{Research: r}
	out := s.observeConnection(ctx, Request{Network: "tcp", Address: "1.2.3.4:443", Research: ref}, "tcp4", a)
	// No control consent: data still forwards, without research records.
	go func() { buf := make([]byte, 3); _, _ = b.Read(buf) }()
	if _, e := out.Write([]byte("off")); e != nil {
		t.Fatal(e)
	}
	r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 2, Enabled: true})
	go func() { buf := make([]byte, 3); _, _ = b.Read(buf); _, _ = b.Write([]byte("reply")) }()
	_, _ = out.Write([]byte("yes"))
	buf := make([]byte, 5)
	_, _ = out.Read(buf)
	out.(*observedConnection).flush(false)
	mu.Lock()
	before := len(records)
	mu.Unlock()
	if before == 0 {
		t.Fatal("no server observation")
	}
	r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 3, Enabled: false})
	go func() { buf := make([]byte, 3); _, _ = b.Read(buf) }()
	_, _ = out.Write([]byte("off"))
	out.(*observedConnection).flush(false)
	mu.Lock()
	if len(records) != before {
		t.Fatal("captured after revoke")
	}
	var d research.FlowData
	_ = json.Unmarshal(records[0].Data, &d)
	first := records[0]
	mu.Unlock()
	if d.Route != "vpn" || first.Origin != "server_gateway" || d.BytesUp != 3 || d.BytesDown != 5 || d.ClientFlow == nil || d.ClientFlow.DeviceID != "device" || d.ClientFlow.FlowID != 7 || d.App.Attribution != "unknown" {
		t.Fatalf("%+v %+v", first, d)
	}
	r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 4, Enabled: true})
	go func() { buf := make([]byte, 2); _, _ = b.Read(buf) }()
	_, _ = out.Write([]byte("on"))
	out.(*observedConnection).flush(false)
	mu.Lock()
	var resumed research.FlowData
	_ = json.Unmarshal(records[len(records)-1].Data, &resumed)
	mu.Unlock()
	if resumed.BytesUp != 2 || !resumed.Incomplete {
		t.Fatal(resumed)
	}
	_ = out.Close()
}
func TestInvalidResearchRefDoesNotBlockVPN(t *testing.T) {
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "u", "d", true }, func(research.Record) bool { t.Fatal("unexpected record"); return false })
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	s := &Server{Research: r}
	ctx := context.WithValue(context.Background(), researchContextKey{}, tls.ConnectionState{})
	if got := s.observeConnection(ctx, Request{Research: &ResearchFlowRef{Version: 99, Session: "bad", FlowID: 1}}, "tcp4", a); got != a {
		t.Fatal("invalid metadata wrapped")
	}
}

func TestServerResearchDNSAndAppProvenance(t *testing.T) {
	var mu sync.Mutex
	var records []research.Record
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "user", "device", true }, func(v research.Record) bool { mu.Lock(); records = append(records, v); mu.Unlock(); return true })
	ref := &ResearchFlowRef{Version: 1, Session: "0123456789abcdef0123456789abcdef", FlowID: 8}
	raw, _ := json.Marshal(research.FlowData{UserID: "forged", App: research.App{Attribution: "known", Packages: []string{"test.app"}}, Protocol: "udp", Destination: "1.1.1.1:53", CounterKind: "delta"})
	r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 1, Enabled: true, Records: []research.Record{{SchemaVersion: 1, ID: "meta", Type: "flow_open", At: time.Now(), Generation: 1, Flow: research.FlowKey{FlowID: 8}, Data: raw}}})
	a, b := net.Pipe()
	defer b.Close()
	s := &Server{Research: r}
	ctx := context.WithValue(context.Background(), researchContextKey{}, tls.ConnectionState{})
	out := s.observeConnection(ctx, Request{Address: "1.1.1.1:53", Research: ref}, "udp4", a).(*observedConnection)
	defer out.Close()
	packet := []byte{0, 42, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1, 'a', 0, 0, 1, 0, 1}
	go func() { buf := make([]byte, 100); _, _ = b.Read(buf) }()
	_, _ = out.Write(packet)
	out.flush(false)
	mu.Lock()
	defer mu.Unlock()
	var flow research.FlowData
	var dns map[string]any
	for _, v := range records {
		if v.Origin != "server_gateway" {
			continue
		}
		if v.Type == "flow_delta" {
			_ = json.Unmarshal(v.Data, &flow)
		}
		if v.Type == "dns" {
			_ = json.Unmarshal(v.Data, &dns)
		}
	}
	if flow.AppSource != "client" || len(flow.App.Packages) != 1 || flow.App.Packages[0] != "test.app" || flow.BytesUp != uint64(len(packet)) || flow.PacketsUp != 1 {
		t.Fatalf("%+v", flow)
	}
	if dns["domain"] != "a" || dns["direction"] != "query" || dns["app_source"] != "client" {
		t.Fatal(dns)
	}
}
func TestServerResearchLeaseAndNonblockingLoss(t *testing.T) {
	var got []research.Record
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "u", "d", true }, func(v research.Record) bool { got = append(got, v); return true })
	ref := ResearchFlowRef{Version: 1, Session: "0123456789abcdef0123456789abcdef", FlowID: 1}
	r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 1, Enabled: true})
	f := &observedConnection{receiver: r, user: "u", device: "d", ref: ref, network: "tcp4", destination: "1.2.3.4:443"}
	f.ready.Store(true)
	f.payload([]byte("ok"), true)
	r.mu.Lock()
	done := make(chan struct{})
	go func() { f.payload([]byte("lost"), true); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("observer blocked VPN")
	}
	r.mu.Unlock()
	f.flush(false)
	var measured research.FlowData
	_ = json.Unmarshal(got[0].Data, &measured)
	if measured.BytesUp != 6 {
		t.Fatalf("contention lost bytes: %d", measured.BytesUp)
	}
	if len(got) != 1 || got[0].Type != "flow_delta" {
		t.Fatal(got)
	}
	r.mu.Lock()
	state := r.sessions["d/"+ref.Session]
	state.until = time.Now().Add(-time.Second)
	r.sessions["d/"+ref.Session] = state
	r.observationStates.Store("d/"+ref.Session, state)
	r.mu.Unlock()
	f.payload([]byte("expired"), true)
	f.flush(false)
	if len(got) != 1 {
		t.Fatal("expired consent observed")
	}
}

func TestAsyncAppMetadataEnrichesExactFlow(t *testing.T) {
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "u", "d", true }, func(research.Record) bool { return true })
	session := "0123456789abcdef0123456789abcdef"
	data, _ := json.Marshal(research.FlowData{App: research.App{Attribution: "unknown"}, Protocol: "udp", Destination: "1.1.1.1:53", CounterKind: "delta"})
	app, _ := json.Marshal(research.App{Attribution: "known", Packages: []string{"late.app"}})
	r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: session, Generation: 1, Enabled: true, Records: []research.Record{
		{SchemaVersion: 1, ID: "f", Type: "flow_open", At: time.Now(), Generation: 1, Flow: research.FlowKey{FlowID: 9}, Data: data},
		{SchemaVersion: 1, ID: "a", Type: "app", At: time.Now(), Generation: 1, Flow: research.FlowKey{FlowID: 9}, Data: app}}})
	r.mu.Lock()
	got := r.apps[appKey(research.FlowKey{DeviceID: "d", SessionID: session, FlowID: 9})]
	r.mu.Unlock()
	if got.app.Attribution != "known" || got.app.Packages[0] != "late.app" || got.destination != "1.1.1.1:53" {
		t.Fatal(got)
	}
}

func TestObserverOpenDoesNotWaitForPolicy(t *testing.T) {
	gate := make(chan struct{})
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { <-gate; return "u", "d", false }, func(research.Record) bool { return true })
	a, b := net.Pipe()
	defer b.Close()
	s := &Server{Research: r}
	ctx := context.WithValue(context.Background(), researchContextKey{}, tls.ConnectionState{})
	done := make(chan net.Conn, 1)
	go func() {
		done <- s.observeConnection(ctx, Request{Research: &ResearchFlowRef{Version: 1, Session: "0123456789abcdef0123456789abcdef", FlowID: 1}}, "tcp4", a)
	}()
	select {
	case out := <-done:
		close(gate)
		_ = out.Close()
	case <-time.After(time.Second):
		close(gate)
		t.Fatal("flow waited for policy I/O")
	}
}
func TestObserverReportsWholeWindowLoss(t *testing.T) {
	var got []research.Record
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "u", "d", true }, func(v research.Record) bool { got = append(got, v); return true })
	ref := ResearchFlowRef{Version: 1, Session: "0123456789abcdef0123456789abcdef", FlowID: 1}
	r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 1, Enabled: true})
	f := &observedConnection{receiver: r, user: "u", device: "d", ref: ref, generation: 1, network: "tcp4"}
	f.dropped.Add(1)
	f.flush(true)
	if len(got) != 1 || got[0].Type != "gap" {
		t.Fatal("whole window loss hidden", got)
	}
}

type finishingWriteConn struct {
	net.Conn
	entered, release chan struct{}
}

func (c *finishingWriteConn) Write(b []byte) (int, error) {
	close(c.entered)
	<-c.release
	return len(b), nil
}
func (c *finishingWriteConn) Close() error { return nil }
func TestObserverCloseDrainsCompletedIO(t *testing.T) {
	var mu sync.Mutex
	var got []research.Record
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "u", "d", true }, func(v research.Record) bool { mu.Lock(); got = append(got, v); mu.Unlock(); return true })
	ref := &ResearchFlowRef{Version: 1, Session: "0123456789abcdef0123456789abcdef", FlowID: 1}
	r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 1, Enabled: true})
	raw := &finishingWriteConn{entered: make(chan struct{}), release: make(chan struct{})}
	ctx := context.WithValue(context.Background(), researchContextKey{}, tls.ConnectionState{})
	out := (&Server{Research: r}).observeConnection(ctx, Request{Research: ref, Address: "1.2.3.4:443"}, "tcp4", raw)
	done := make(chan struct{})
	go func() { _, _ = out.Write([]byte("last")); close(done) }()
	<-raw.entered
	_ = out.Close()
	time.Sleep(50 * time.Millisecond)
	close(raw.release)
	<-done
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		n := r.observers
		r.mu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	var count uint64
	for _, v := range got {
		var d research.FlowData
		_ = json.Unmarshal(v.Data, &d)
		count += d.BytesUp
	}
	if count != 4 {
		t.Fatalf("last I/O lost: %d", count)
	}
}

func TestObserverExistingFlowCanOptInAfterIdentityResolution(t *testing.T) {
	var got []research.Record
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "u", "d", true }, func(v research.Record) bool { got = append(got, v); return true })
	ref := &ResearchFlowRef{Version: 1, Session: "0123456789abcdef0123456789abcdef", FlowID: 1}
	a, b := net.Pipe()
	defer b.Close()
	ctx := context.WithValue(context.Background(), researchContextKey{}, tls.ConnectionState{})
	f := (&Server{Research: r}).observeConnection(ctx, Request{Research: ref, Address: "1.2.3.4:443"}, "tcp4", a).(*observedConnection)
	defer f.Close()
	deadline := time.Now().Add(time.Second)
	for !f.ready.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !f.ready.Load() {
		t.Fatal("asynchronous identity never published")
	}
	f.payload([]byte("off"), true)
	r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 1, Enabled: true})
	f.payload([]byte("on"), true)
	f.flush(false)
	if len(got) != 1 {
		t.Fatal(got)
	}
	var d research.FlowData
	_ = json.Unmarshal(got[0].Data, &d)
	if d.BytesUp != 2 {
		t.Fatal(d)
	}
}

func TestObserverDNSQueueLossKeepsByteCounters(t *testing.T) {
	var got []research.Record
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "u", "d", true }, func(v research.Record) bool { got = append(got, v); return true })
	ref := ResearchFlowRef{Version: 1, Session: "0123456789abcdef0123456789abcdef", FlowID: 1}
	r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 1, Enabled: true})
	f := &observedConnection{receiver: r, user: "u", device: "d", ref: ref, generation: 1, network: "udp4", destination: "1.1.1.1:53"}
	f.ready.Store(true)
	packet := []byte{0, 42, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1, 'a', 0, 0, 1, 0, 1}
	for range 17 {
		f.payload(packet, true)
	}
	f.flush(false)
	var data research.FlowData
	_ = json.Unmarshal(got[0].Data, &data)
	if data.BytesUp != uint64(17*len(packet)) || data.PacketsUp != 17 || len(got) != 18 || got[17].Type != "gap" {
		t.Fatal(data, len(got))
	}
}
func TestObserverPolicyRecheckedAndWriterLossVisible(t *testing.T) {
	policy := true
	fail := true
	var got []research.Record
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "u", "d", policy }, func(v research.Record) bool {
		if fail {
			fail = false
			return false
		}
		got = append(got, v)
		return true
	})
	ref := ResearchFlowRef{Version: 1, Session: "0123456789abcdef0123456789abcdef", FlowID: 1}
	r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 1, Enabled: true})
	f := &observedConnection{receiver: r, user: "u", device: "d", ref: ref, generation: 1, network: "tcp4", destination: "1.2.3.4:443"}
	f.ready.Store(true)
	f.payload([]byte("a"), true)
	f.flush(false)
	if len(got) != 1 || got[0].Type != "gap" {
		t.Fatal("writer loss hidden", got)
	}
	f.payload([]byte("b"), true)
	policy = false
	f.flush(false)
	if len(got) != 1 {
		t.Fatal("server revoke ignored")
	}
}

// A restart loses the control lease, not the meaning of the client flow ID.
// New consent may resume observation under a fresh server epoch.
func TestObserverRestartRequiresConsentAndSeparatesEpochs(t *testing.T) {
	opts := research.StoreOptions{Dir: t.TempDir(), MaxBytes: 1 << 20}
	store, err := research.OpenStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	var got []research.Record
	identity := func(tls.ConnectionState) (string, string, bool) { return "u", "d", true }
	writer := func(v research.Record) bool { got = append(got, v); return store.TryAppend(v) }
	ref := ResearchFlowRef{Version: 1, Session: "0123456789abcdef0123456789abcdef", FlowID: 1}
	var previousEpoch string
	for restart := 0; restart < 2; restart++ {
		if restart > 0 {
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = research.OpenStore(opts)
			if err != nil {
				t.Fatal(err)
			}
		}
		r := NewResearchReceiver(identity, writer)
		f := &observedConnection{receiver: r, user: "u", device: "d", id: 1, ref: ref, network: "tcp4", destination: "1.2.3.4:443"}
		f.ready.Store(true)
		before := len(got)
		f.payload([]byte("without new control"), true)
		f.flush(false)
		if len(got) != before {
			t.Fatal("restart inherited consent")
		}
		r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 10, Enabled: true})
		f.payload([]byte("yes"), true)
		f.flush(false)
		if len(got) != before+1 {
			t.Fatal("fresh consent not applied")
		}
		row := got[len(got)-1]
		var d research.FlowData
		if err := json.Unmarshal(row.Data, &d); err != nil {
			t.Fatal(err)
		}
		if d.BytesUp != 3 || d.ClientFlow == nil || d.ClientFlow.SessionID != ref.Session {
			t.Fatal(d)
		}
		if row.Flow.SessionID == previousEpoch || row.Flow.SessionID == ref.Session {
			t.Fatal("server epochs collided")
		}
		previousEpoch = row.Flow.SessionID
		r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 11, Enabled: false})
		r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: ref.Session, Generation: 10, Enabled: true})
		f.payload([]byte("stale path"), true)
		f.flush(false)
		if len(got) != before+1 {
			t.Fatal("stale path restored revoked consent")
		}
	}
	if got[0].ID == got[1].ID {
		t.Fatal("records deduplicate across restart")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = research.OpenStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.Query(context.Background(), research.Query{Origin: "server_gateway", DeviceID: "d"})
	if err != nil || len(page.Records) != 2 {
		t.Fatalf("restart history: %d %v", len(page.Records), err)
	}
	for _, row := range page.Records {
		var d research.FlowData
		if err := json.Unmarshal(row.Data, &d); err != nil {
			t.Fatal(err)
		}
		if row.Origin != "server_gateway" || row.ReceivedAt == "" || d.ClientFlow == nil || d.ClientFlow.SessionID != ref.Session || d.BytesUp != 3 {
			t.Fatalf("provenance changed after reopening: %+v %+v", row, d)
		}
	}

}
