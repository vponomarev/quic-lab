package mdm

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func phaseWire() map[string]any {
	row := map[string]any{"id": "node", "name": "Node", "endpoint": "example.org:443", "outcome": "success", "relayOutcome": "not_applicable", "echoSucceeded": 3}
	return map[string]any{"schema": 2, "runId": "local-run", "sourceId": "group", "fingerprint": strings.Repeat("a", 64), "sequence": 2, "network": "cell", "initiator": "local", "state": "finished", "startedAt": testNow, "finishedAt": testNow,
		"phases": map[string]any{"wifi": map[string]any{"network": "wifi", "state": "finished", "rows": []any{row}}, "cell": map[string]any{"network": "cell", "state": "skipped", "reason": "permission_denied", "rows": []any{map[string]any{"id": "node", "name": "Node", "endpoint": "example.org:443", "outcome": "permission_denied", "relayOutcome": "not_applicable"}}}}}
}
func decodePhase(t *testing.T, w map[string]any) ProbeSnapshot {
	t.Helper()
	raw, e := json.Marshal(w)
	if e != nil {
		t.Fatal(e)
	}
	var p ProbeSnapshot
	if e = json.Unmarshal(raw, &p); e != nil {
		t.Fatal(e)
	}
	return p
}
func phaseReport(t *testing.T, capability bool) (*Store, Binding, DeviceReport) {
	t.Helper()
	s, b, r := reportFixture(t)
	rights := Rights{Config: true, VPN: true, Telemetry: true}
	if _, e := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &rights}, testNow); e != nil {
		t.Fatal(e)
	}
	r.Capabilities = append(r.Capabilities, "vless_probe_v1")
	if capability {
		r.Capabilities = append(r.Capabilities, "vless_probe_phases_v2")
	}
	r.ProbeSources = []ProbeSource{{ID: "group", Name: "Group", Fingerprint: strings.Repeat("a", 64)}}
	return s, b, r
}
func TestProbePhasesReportRoundTrip(t *testing.T) {
	s, b, r := phaseReport(t, true)
	r.ProbeSnapshots = []ProbeSnapshot{decodePhase(t, phaseWire())}
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	state, e := s.DeviceState(b.ID)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(state.Probes["group"])
	var got map[string]any
	json.Unmarshal(raw, &got)
	if got["schema"] != float64(2) || got["phases"] == nil {
		t.Fatalf("phase data lost: %s", raw)
	}
	phases := got["phases"].(map[string]any)
	if phases["cell"].(map[string]any)["reason"] != "permission_denied" {
		t.Fatal("skip reason lost")
	}
	p := decodePhase(t, phaseWire())
	p.Sequence = 1
	p.State = "running"
	if e = s.SaveProbe(b.ID, b.Epoch, p, testNow.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	state, _ = s.DeviceState(b.ID)
	if state.Probes["group"].State != "finished" || !state.Probes["group"].ReceivedAt.Equal(testNow) {
		t.Fatal("stale evidence replaced")
	}
	rights := Rights{Config: true, VPN: true}
	s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &rights}, testNow)
	p.Sequence = 3
	if e = s.SaveProbe(b.ID, b.Epoch, p, testNow); e == nil {
		t.Fatal("revoked telemetry accepted")
	}
}
func TestProbePhasesRejectMalformed(t *testing.T) {
	cases := map[string]func(map[string]any){
		"unknown_schema":     func(w map[string]any) { w["schema"] = 3 },
		"legacy_with_phases": func(w map[string]any) { delete(w, "schema") },
		"no_phases":          func(w map[string]any) { delete(w, "phases") },
		"remote_v2":          func(w map[string]any) { w["initiator"] = "mdm" },
		"unknown_network":    func(w map[string]any) { w["network"] = "ethernet" },
		"wrong_source":       func(w map[string]any) { w["fingerprint"] = strings.Repeat("b", 64) },
		"third_phase":        func(w map[string]any) { p := w["phases"].(map[string]any); p["ethernet"] = p["wifi"] },
		"network_mismatch":   func(w map[string]any) { w["phases"].(map[string]any)["cell"].(map[string]any)["network"] = "wifi" },
		"bad_reason":         func(w map[string]any) { w["phases"].(map[string]any)["cell"].(map[string]any)["reason"] = "made_up" },
		"skipped_success": func(w map[string]any) {
			w["phases"].(map[string]any)["cell"].(map[string]any)["rows"].([]any)[0].(map[string]any)["outcome"] = "success"
		},
		"different_nodes": func(w map[string]any) {
			w["phases"].(map[string]any)["cell"].(map[string]any)["rows"].([]any)[0].(map[string]any)["id"] = "other"
		},
		"duplicate_rows": func(w map[string]any) {
			p := w["phases"].(map[string]any)["wifi"].(map[string]any)
			r := p["rows"].([]any)
			p["rows"] = append(r, r[0])
		},
		"too_many_rows": func(w map[string]any) {
			p := w["phases"].(map[string]any)["wifi"].(map[string]any)
			p["rows"] = make([]any, 257)
		},
		"top_rows": func(w map[string]any) { w["rows"] = w["phases"].(map[string]any)["wifi"].(map[string]any)["rows"] },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, b, r := phaseReport(t, true)
			w := phaseWire()
			mutate(w)
			r.ProbeSnapshots = []ProbeSnapshot{decodePhase(t, w)}
			if e := s.Report(b.ID, b.Epoch, r, testNow); e == nil {
				t.Fatal("malformed phases accepted")
			}
		})
	}
}
func TestProbePhasesRequireCapability(t *testing.T) {
	s, b, r := phaseReport(t, false)
	r.ProbeSnapshots = []ProbeSnapshot{decodePhase(t, phaseWire())}
	if e := s.Report(b.ID, b.Epoch, r, testNow); e == nil {
		t.Fatal("unnegotiated phases accepted")
	}
}

func TestProbePhasesInProgressAndCanceledStates(t *testing.T) {
	for _, state := range []string{"queued", "running", "canceled", "interrupted", "network_changed", "permission_denied", "budget"} {
		t.Run(state, func(t *testing.T) {
			s, b, r := phaseReport(t, true)
			w := phaseWire()
			p := w["phases"].(map[string]any)["cell"].(map[string]any)
			p["state"] = state
			delete(p, "reason")
			p["rows"].([]any)[0].(map[string]any)["outcome"] = state
			r.ProbeSnapshots = []ProbeSnapshot{decodePhase(t, w)}
			if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
				t.Fatal("actual Android phase rejected", e)
			}
		})
	}
}

func TestProbePhasesPermissionRevokedBeforeWorkerStarts(t *testing.T) {
	s, b, r := phaseReport(t, true)
	w := phaseWire()
	phase := w["phases"].(map[string]any)["cell"].(map[string]any)
	phase["state"] = "finished"
	delete(phase, "reason")
	r.ProbeSnapshots = []ProbeSnapshot{decodePhase(t, w)}
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal("worker denial rejected", e)
	}
}
func TestProbePhasesCapabilityAdvertised(t *testing.T) {
	s, b, _ := phaseReport(t, true)
	v, e := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch}, testNow)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range v.Capabilities {
		if c == "vless_probe_phases_v2" {
			return
		}
	}
	t.Fatal("missing phases capability")
}

func TestProbePhasesRejectLegacyRemoteCommands(t *testing.T) {
	s, b, r := phaseReport(t, true)
	w := phaseWire()
	w["state"] = "running"
	r.ProbeSnapshots = []ProbeSnapshot{decodePhase(t, w)}
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	for _, action := range []string{"retry", "cancel"} {
		_, e := s.QueueProbe(b.ID, "request-"+action, ProbeRequest{SourceID: "group", Fingerprint: strings.Repeat("a", 64), Action: action, RunID: "local-run"}, testNow)
		if e == nil {
			t.Errorf("legacy %s accepted for phased run", action)
		}
	}
	if _, e := s.QueueProbe(b.ID, "request-start", ProbeRequest{SourceID: "group", Fingerprint: strings.Repeat("a", 64), Action: "start"}, testNow); e != nil {
		t.Fatal("legacy start regressed", e)
	}
}
