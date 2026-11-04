package mdm

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sequenceRequest(t *testing.T, action, run, mode string) ProbeRequest {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"sourceId": "group", "fingerprint": strings.Repeat("a", 64), "action": action, "runId": run, "mode": mode})
	var r ProbeRequest
	if e := json.Unmarshal(raw, &r); e != nil {
		t.Fatal(e)
	}
	return r
}
func sequenceFixture(t *testing.T) (*Store, Binding, DeviceReport) {
	t.Helper()
	s, b, r := phaseReport(t, true)
	r.Capabilities = append(r.Capabilities, "vless_probe_sequence_v2")
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	return s, b, r
}
func TestProbeSequenceNegotiationAndDowngrade(t *testing.T) {
	s, b, r := sequenceFixture(t)
	req := sequenceRequest(t, "start", "", "phased")
	c, e := s.QueueProbe(b.ID, "start-sequence", req, testNow)
	if e != nil {
		t.Fatal(e)
	}
	if c.Kind != "vless_probe_sequence" {
		t.Fatal("wrong command kind", c.Kind)
	}
	again, e := s.QueueProbe(b.ID, "start-sequence", req, testNow)
	if e != nil || again.ID != c.ID || c.ExpiresAt.Sub(c.IssuedAt) != 5*time.Minute {
		t.Fatal("idempotency/TTL", e)
	}
	sync, e := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, Capabilities: []string{ProbeSequenceCapability}}, testNow)
	if e != nil {
		t.Fatal(e)
	}
	advertised := false
	for _, v := range sync.Capabilities {
		advertised = advertised || v == "vless_probe_sequence_v2"
	}
	if !advertised {
		t.Fatal("missing capability")
	}
	if len(sync.Commands) != 1 || sync.Commands[0].ID != c.ID {
		t.Fatal("negotiated sequence missing")
	}
	r.Sequence++
	r.Capabilities = []string{"web-control-v1", "vless_probe_v1", "vless_probe_phases_v2"}
	if e = s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	sync, _ = s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, Capabilities: []string{ProbeSequenceCapability}}, testNow)
	for _, command := range sync.Commands {
		if command.ID == c.ID {
			t.Fatal("downgraded client got sequence")
		}
	}
	if _, e = s.QueueProbe(b.ID, "new-sequence", req, testNow); e == nil {
		t.Fatal("unsupported client accepted")
	}
	if _, e = s.QueueProbe(b.ID, "bad-mode", sequenceRequest(t, "start", "", "invalid"), testNow); e == nil {
		t.Fatal("unknown mode accepted")
	}
}
func TestProbeSequenceSyncNegotiatesBeforeDowngradeReport(t *testing.T) {
	s, b, _ := sequenceFixture(t)
	phased, e := s.QueueProbe(b.ID, "phased", sequenceRequest(t, "start", "", "phased"), testNow)
	if e != nil {
		t.Fatal(e)
	}
	legacy, e := s.QueueProbe(b.ID, "legacy", sequenceRequest(t, "start", "", ""), testNow)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name, caps string
		want       bool
	}{
		{"current", `,"capabilities":["vless_probe_sequence_v2"]`, true},
		{"downgraded-before-report", "", false},
		{"unrelated-capability", `,"capabilities":["vless_probe_v1"]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"version": 1, "epoch": b.Epoch})
			var req SyncRequest
			if e := json.Unmarshal([]byte(strings.TrimSuffix(string(raw), "}")+tc.caps+"}"), &req); e != nil {
				t.Fatal(e)
			}
			response, e := s.Sync(b.ID, req, testNow)
			if e != nil {
				t.Fatal(e)
			}
			got, gotLegacy := false, false
			for _, c := range response.Commands {
				got = got || c.ID == phased.ID
				gotLegacy = gotLegacy || c.ID == legacy.ID
			}
			if got != tc.want || !gotLegacy {
				t.Fatalf("sequence=%v want=%v legacy=%v", got, tc.want, gotLegacy)
			}
		})
	}
}
func TestProbeSequenceReportRequiresMatchingStartAndInterruptsPhases(t *testing.T) {
	s, b, _ := sequenceFixture(t)
	cmd, e := s.QueueProbe(b.ID, "start", sequenceRequest(t, "start", "", "phased"), testNow)
	if e != nil {
		t.Fatal(e)
	}
	w := phaseWire()
	w["runId"] = cmd.ID
	w["initiator"] = "mdm"
	w["state"] = "running"
	cell := w["phases"].(map[string]any)["cell"].(map[string]any)
	cell["state"] = "queued"
	delete(cell, "reason")
	cell["rows"].([]any)[0].(map[string]any)["outcome"] = "queued"
	p := decodePhase(t, w)
	if e = s.SaveProbe(b.ID, b.Epoch, p, testNow); e != nil {
		t.Fatal("valid remote sequence rejected", e)
	}
	for _, action := range []string{"retry", "cancel"} {
		if _, e = s.QueueProbe(b.ID, action, sequenceRequest(t, action, cmd.ID, "phased"), testNow); e != nil {
			t.Fatal(action, e)
		}
	}
	cancel, e := s.QueueProbe(b.ID, "cancel-not-start", sequenceRequest(t, "cancel", cmd.ID, "phased"), testNow)
	if e != nil {
		t.Fatal(e)
	}
	p.RunID = cancel.ID
	p.StartedAt = testNow.Add(time.Second)
	if e = s.SaveProbe(b.ID, b.Epoch, p, testNow); e == nil {
		t.Fatal("cancel used as start provenance")
	}
	legacy, e := s.QueueProbe(b.ID, "legacy", sequenceRequest(t, "start", "", ""), testNow)
	if e != nil {
		t.Fatal(e)
	}
	p.RunID = legacy.ID
	if e = s.SaveProbe(b.ID, b.Epoch, p, testNow); e == nil {
		t.Fatal("legacy command accepted phased evidence")
	}
	if _, e = s.Activate(b.ID, testNow.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	state, _ := s.DeviceState(b.ID)
	raw, _ := json.Marshal(state.Probes["group"])
	var got map[string]any
	json.Unmarshal(raw, &got)
	phase := got["phases"].(map[string]any)["cell"].(map[string]any)
	if state.Probes["group"].State != "interrupted" || phase["state"] != "interrupted" || phase["rows"].([]any)[0].(map[string]any)["outcome"] != "interrupted" {
		t.Fatal("old epoch phase still active", string(raw))
	}
}

func TestProbeSequenceRetryIdempotencyAfterNewSnapshot(t *testing.T) {
	s, b, _ := sequenceFixture(t)
	start, e := s.QueueProbe(b.ID, "start", sequenceRequest(t, "start", "", "phased"), testNow)
	if e != nil {
		t.Fatal(e)
	}
	w := phaseWire()
	w["runId"] = start.ID
	w["initiator"] = "mdm"
	p := decodePhase(t, w)
	if e = s.SaveProbe(b.ID, b.Epoch, p, testNow); e != nil {
		t.Fatal(e)
	}
	req := sequenceRequest(t, "retry", start.ID, "phased")
	retry, e := s.QueueProbe(b.ID, "retry-once", req, testNow)
	if e != nil {
		t.Fatal(e)
	}
	p.RunID = retry.ID
	p.StartedAt = testNow.Add(time.Second)
	p.Sequence = 1
	if e = s.SaveProbe(b.ID, b.Epoch, p, testNow.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	repeated, e := s.QueueProbe(b.ID, "retry-once", req, testNow.Add(2*time.Second))
	if e != nil || repeated.ID != retry.ID {
		t.Fatal("duplicate request lost idempotency after report", e)
	}
}
