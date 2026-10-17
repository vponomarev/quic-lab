package mdm

import (
	"strings"
	"testing"
	"time"
)

func TestProbeCommands(t *testing.T) {
	s, b, r := reportFixture(t)
	rights := Rights{Config: true, VPN: true, Telemetry: true}
	s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &rights}, testNow)
	r.Capabilities = append(r.Capabilities, "vless_probe_v1")
	r.ProbeSources = []ProbeSource{{ID: "group", Fingerprint: strings.Repeat("a", 64), Name: "Group"}}
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	req := ProbeRequest{SourceID: "group", Fingerprint: strings.Repeat("a", 64), Action: "start"}
	c, e := s.QueueProbe(b.ID, "request1", req, testNow)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.QueueProbe(b.ID, "request1", req, testNow)
	if e != nil || c.ID != again.ID || c.ExpiresAt.Sub(c.IssuedAt) != 5*time.Minute {
		t.Fatal("idempotency/TTL", e)
	}
	r.Sequence++
	r.Capabilities = []string{"web-control-v1"}
	s.Report(b.ID, b.Epoch, r, testNow)
	out, e := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch}, testNow)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range out.Commands {
		if c.Kind == "vless_probe" {
			t.Fatal("old client got unknown command")
		}
	}
	if _, e = s.QueueProbe(b.ID, "request2", req, testNow); e == nil {
		t.Fatal("unsupported client accepted")
	}
}
func TestProbeSnapshots(t *testing.T) {
	s, b, r := reportFixture(t)
	rights := Rights{Config: true, VPN: true, Telemetry: true}
	s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &rights}, testNow)
	r.Capabilities = append(r.Capabilities, "vless_probe_v1")
	r.ProbeSources = []ProbeSource{{ID: "group", Fingerprint: strings.Repeat("a", 64), Name: "Group"}}
	s.Report(b.ID, b.Epoch, r, testNow)
	p := ProbeSnapshot{RunID: "run1", SourceID: "group", Fingerprint: strings.Repeat("a", 64), Sequence: 2, Network: "wifi", Initiator: "local", State: "finished", StartedAt: testNow, FinishedAt: testNow, Rows: []ProbeRow{{ID: "node", Name: "Node", Endpoint: "example.org:443", Outcome: "success", RelayOutcome: "not_applicable", EchoSucceeded: 3}}}
	if e := s.SaveProbe(b.ID, b.Epoch, p, testNow); e != nil {
		t.Fatal(e)
	}
	p.Sequence = 1
	p.State = "running"
	if e := s.SaveProbe(b.ID, b.Epoch, p, testNow); e != nil {
		t.Fatal(e)
	}
	state, _ := s.DeviceState(b.ID)
	if state.Probes["group"].State != "finished" {
		t.Fatal("stale snapshot overwrote")
	}
	p.Sequence = 3
	p.Rows = make([]ProbeRow, 257)
	if e := s.SaveProbe(b.ID, b.Epoch, p, testNow); e == nil {
		t.Fatal("oversize accepted")
	}
	rights.Telemetry = false
	s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &rights}, testNow)
	p.Rows = nil
	if e := s.SaveProbe(b.ID, b.Epoch, p, testNow); e == nil {
		t.Fatal("revoked consent accepted")
	}
}
