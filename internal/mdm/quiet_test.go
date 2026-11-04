package mdm

import (
	"encoding/json"
	"testing"
)

func TestQuietModeReportRoundTripAndValidation(t *testing.T) {
	for _, mode := range []string{"", "continuous", "quiet", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			s, b, r := reportFixture(t)
			raw, _ := json.Marshal(r)
			fields := map[string]json.RawMessage{}
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			fields["backgroundMode"], _ = json.Marshal(mode)
			raw, _ = json.Marshal(fields)
			if err := json.Unmarshal(raw, &r); err != nil {
				t.Fatal(err)
			}
			err := s.Report(b.ID, b.Epoch, r, testNow)
			if mode == "invalid" {
				if err == nil {
					t.Fatal("invalid background mode accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			state, err := s.DeviceState(b.ID)
			if err != nil {
				t.Fatal(err)
			}
			wire, _ := json.Marshal(state.Report)
			var saved map[string]any
			json.Unmarshal(wire, &saved)
			if mode != "" && saved["backgroundMode"] != mode {
				t.Fatalf("reported mode lost: %v", saved["backgroundMode"])
			}
			if state.Report.AppliedRevision != 0 || !state.ReceivedAt.Equal(testNow) {
				t.Fatal("report changed application/last-contact semantics")
			}
		})
	}
}
func TestQuietModeServerCapability(t *testing.T) {
	s, b, _ := reportFixture(t)
	response, err := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range response.Capabilities {
		if c == "mdm_quiet_v1" {
			return
		}
	}
	t.Fatal("quiet report capability missing")
}
