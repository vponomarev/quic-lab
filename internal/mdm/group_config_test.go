package mdm

import (
	"encoding/json"
	"errors"
	"testing"
)

const groupMember = "11111111-1111-4111-8111-111111111111"
const groupOther = "22222222-2222-4222-8222-222222222222"

func groupDocument() ConfigurationDocument {
	var d ConfigurationDocument
	_ = json.Unmarshal([]byte(validDocument), &d)
	d.Profiles[0].Settings["automatic_group_members"] = json.RawMessage(`["` + groupMember + `"]`)
	d.Profiles = append(d.Profiles, ConfigurationProfile{ID: groupMember, Name: "Member", Settings: map[string]json.RawMessage{"transport": json.RawMessage(`"awg"`)}})
	return d
}
func TestAutomaticGroupReferences(t *testing.T) {
	raw, _ := json.Marshal(groupDocument())
	if err := ValidateDocument(raw); err != nil {
		t.Fatal("valid mixed group rejected", err)
	}
	for _, tc := range []string{"missing", "self", "duplicate", "nested", "member-enabled", "overlap", "not-array", "null", "invalid-id", "too-many"} {
		t.Run(tc, func(t *testing.T) {
			d := groupDocument()
			switch tc {
			case "missing":
				d.Profiles = d.Profiles[:1]
			case "self":
				d.Profiles[0].Settings["automatic_group_members"] = json.RawMessage(`["default"]`)
			case "duplicate":
				d.Profiles[0].Settings["automatic_group_members"] = json.RawMessage(`["` + groupMember + `","` + groupMember + `"]`)
			case "nested":
				d.Profiles[1].Settings["automatic_group_members"] = json.RawMessage(`["default"]`)
			case "member-enabled":
				d.Multiple = true
				d.EnabledProfileIDs = []string{"default", groupMember}
			case "overlap":
				d.Profiles = append(d.Profiles, ConfigurationProfile{ID: groupOther, Name: "Other", Settings: map[string]json.RawMessage{"automatic_group_members": json.RawMessage(`["` + groupMember + `"]`)}})
				d.Multiple = true
				d.EnabledProfileIDs = []string{"default", groupOther}
			case "not-array":
				d.Profiles[0].Settings["automatic_group_members"] = json.RawMessage(`"bad"`)
			case "null":
				d.Profiles[0].Settings["automatic_group_members"] = json.RawMessage(`null`)
			case "invalid-id":
				d.Profiles[0].Settings["automatic_group_members"] = json.RawMessage(`["bad"]`)
			case "too-many":
				a := make([]string, 32)
				for i := range a {
					a[i] = groupMember
				}
				d.Profiles[0].Settings["automatic_group_members"], _ = json.Marshal(a)
			}
			raw, _ := json.Marshal(d)
			if ValidateDocument(raw) == nil {
				t.Fatal("invalid group accepted")
			}
		})
	}
}
func TestAutomaticGroupCapabilityAndDowngrade(t *testing.T) {
	s, b, r := reportFixture(t)
	if err := s.Report(b.ID, b.Epoch, r, testNow); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(groupDocument())
	if _, err := s.SetDesired(b.ID, 0, "current", raw); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("old client accepted group", err)
	}
	if _, err := s.SetDesiredChecked(b.ID, 0, 3, "old-group", "current", raw); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("old checked client accepted group", err)
	}
	r.Sequence++
	r.Capabilities = append(r.Capabilities, "automatic_groups_v1")
	if err := s.Report(b.ID, b.Epoch, r, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDesiredChecked(b.ID, 0, 3, "new-group", "current", raw); err != nil {
		t.Fatal(err)
	}
	req := SyncRequest{Version: 1, Epoch: b.Epoch}
	got, err := s.Sync(b.ID, req, testNow)
	if err != nil || got.DesiredConfig == nil {
		t.Fatal("capable client missing group", err)
	}
	found := false
	for _, c := range got.Capabilities {
		found = found || c == "automatic_groups_v1"
	}
	if !found {
		t.Fatal("server capability absent")
	}
	r.Sequence++
	r.Capabilities = []string{"web-control-v1"}
	if err := s.Report(b.ID, b.Epoch, r, testNow); err != nil {
		t.Fatal(err)
	}
	got, err = s.Sync(b.ID, req, testNow)
	if err != nil || got.DesiredConfig != nil {
		t.Fatal("downgraded client received group", err)
	}
}

func TestAutomaticGroupMixedRelayReferences(t *testing.T) {
	for _, transport := range []string{"awg", "quic", "https"} {
		t.Run(transport, func(t *testing.T) {
			d := relayDocument(t)
			d.Profiles[1].Settings["transport"], _ = json.Marshal(transport)
			d.Profiles[1].Settings[AutomaticGroupMembers] = json.RawMessage(`["` + groupOther + `"]`)
			d.Profiles = append(d.Profiles, ConfigurationProfile{ID: groupOther, Name: "Member", Settings: map[string]json.RawMessage{"transport": json.RawMessage(`"vless"`)}})
			raw, _ := json.Marshal(d)
			if err := ValidateDocument(raw); err != nil {
				t.Fatal("supported mixed HA group rejected", err)
			}
		})
	}
}
