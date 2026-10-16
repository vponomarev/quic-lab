package mdm

import (
	"encoding/json"
	"testing"
)

func TestProfileSelectionPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, policy string
		valid        bool
	}{
		{"enabled", `{"enabled":true,"profileIds":["default"],"serverRevision":0}`, true},
		{"disabled", `{"enabled":false,"profileIds":[],"serverRevision":0}`, true},
		{"missing", `{"enabled":true,"profileIds":["missing"],"serverRevision":0}`, false},
		{"duplicate", `{"enabled":true,"profileIds":["default","default"],"serverRevision":0}`, false},
		{"empty", `{"enabled":true,"profileIds":[],"serverRevision":0}`, false},
		{"negative", `{"enabled":true,"profileIds":["default"],"serverRevision":-1}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var d map[string]json.RawMessage
			json.Unmarshal([]byte(validDocument), &d)
			d["profileSelection"] = json.RawMessage(tc.policy)
			raw, _ := json.Marshal(d)
			if (ValidateDocument(raw) == nil) != tc.valid {
				t.Fatal("validation mismatch")
			}
		})
	}
}
func TestProfileSelectionCapability(t *testing.T) {
	raw := json.RawMessage(`{"profileSelection":{"enabled":true,"profileIds":["default"]}}`)
	if allowsProfileSelection(bindingState{}, raw) {
		t.Fatal("old client accepted")
	}
	if !allowsProfileSelection(bindingState{}, json.RawMessage(validDocument)) {
		t.Fatal("legacy rejected")
	}
}

func TestProfileSelectionWaitsForClientUpgrade(t *testing.T) {
	s, b, r := reportFixture(t)
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	var d map[string]json.RawMessage
	json.Unmarshal([]byte(validDocument), &d)
	d["profileSelection"] = json.RawMessage(`{"enabled":true,"profileIds":["default"],"serverRevision":0}`)
	raw, _ := json.Marshal(d)
	if _, e := s.SetDesiredChecked(b.ID, 0, r.ConfigGeneration, "selection-test", "current", raw); e != nil {
		t.Fatal(e)
	}
	out, e := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch}, testNow)
	if e != nil || out.DesiredConfig != nil {
		t.Fatal("sent unsupported document", e)
	}
	r.Sequence++
	r.Capabilities = append(r.Capabilities, "profile_selection_v1")
	if e = s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	out, e = s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch}, testNow)
	if e != nil || out.DesiredConfig == nil {
		t.Fatal("upgrade did not release pending permission", e)
	}
}
