package configtransfer

import (
	"encoding/json"
	"quiclab/internal/mdm"
	"testing"
)

const memberID = "11111111-1111-4111-8111-111111111111"
const newRootID = "22222222-2222-4222-8222-222222222222"
const newMemberID = "33333333-3333-4333-8333-333333333333"

func groupedFixture() []byte {
	var d mdm.ConfigurationDocument
	_ = json.Unmarshal([]byte(fixture), &d)
	d.Profiles[0].Settings["automatic_group_members"] = json.RawMessage(`["` + memberID + `"]`)
	d.Profiles = append(d.Profiles, mdm.ConfigurationProfile{ID: memberID, Name: "Member", Settings: map[string]json.RawMessage{"transport": json.RawMessage(`"vless"`)}})
	raw, _ := json.Marshal(d)
	return raw
}
func TestGroupReferencesRemapAcrossImportModes(t *testing.T) {
	for _, mode := range []string{"add", "update", "replace"} {
		t.Run(mode, func(t *testing.T) {
			sections := []string{"profiles"}
			if mode == "replace" {
				sections = []string{"profiles", "routing", "network", "diagnostics"}
			}
			raw, err := Export(groupedFixture(), Selection{Sections: sections}, false)
			if err != nil {
				t.Fatal("group export", err)
			}
			base := []byte(fixture)
			root, member := newRootID, newMemberID
			if mode == "update" {
				base = groupedFixture()
				root, member = memberID, "default"
			}
			p, err := Prepare(base, raw, ImportOptions{Mode: mode, Mapping: map[string]string{"default": root, memberID: member}})
			if err != nil {
				t.Fatal(err)
			}
			var d mdm.ConfigurationDocument
			_ = json.Unmarshal(p.Candidate, &d)
			found := false
			for _, profile := range d.Profiles {
				if profile.ID == root {
					found = true
					var ids []string
					_ = json.Unmarshal(profile.Settings["automatic_group_members"], &ids)
					if len(ids) != 1 || ids[0] != member {
						t.Fatal("member references not remapped", ids)
					}
				}
			}
			if !found {
				t.Fatal("root absent")
			}
		})
	}
}
func TestPartialGroupNeedsExplicitDependencyMapping(t *testing.T) {
	raw, err := Export(groupedFixture(), Selection{Sections: []string{"profiles"}, ProfileIDs: []string{"default"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	mapping := map[string]string{"default": newRootID}
	if _, err = Prepare(groupedFixture(), raw, ImportOptions{Mode: "add", Mapping: mapping}); err == nil {
		t.Fatal("dependency silently matched by source ID")
	}
	mapping[memberID] = memberID
	p, err := Prepare(groupedFixture(), raw, ImportOptions{Mode: "add", Mapping: mapping})
	if err != nil {
		t.Fatal("explicit dependency rejected", err)
	}
	if mdm.ValidateDocument(p.Candidate) != nil {
		t.Fatal("invalid mapped candidate")
	}
	mapping[memberID] = newRootID
	if _, err = Prepare(groupedFixture(), raw, ImportOptions{Mode: "add", Mapping: mapping}); err == nil {
		t.Fatal("colliding mapping accepted")
	}
}
func TestRelayTargetRemappedWithProfiles(t *testing.T) {
	var d mdm.ConfigurationDocument
	_ = json.Unmarshal([]byte(fixture), &d)
	d.Profiles[0].Settings["https_endpoint"] = json.RawMessage(`"vpn.example:443"`)
	d.Profiles = append(d.Profiles, mdm.ConfigurationProfile{ID: memberID, Name: "Relay", Settings: map[string]json.RawMessage{"transport": json.RawMessage(`"vless"`), "relay_exit_profile_id": json.RawMessage(`"default"`)}})
	source, _ := json.Marshal(d)
	raw, err := Export(source, Selection{Sections: []string{"profiles"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	// Existing default is a valid-looking but unrelated target. Never bind to it implicitly.
	base := source
	p, err := Prepare(base, raw, ImportOptions{Mode: "add", Mapping: map[string]string{"default": newRootID, memberID: newMemberID}})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(p.Candidate, &d)
	var target string
	for _, profile := range d.Profiles {
		if profile.ID == newMemberID {
			_ = json.Unmarshal(profile.Settings["relay_exit_profile_id"], &target)
		}
	}
	if target != newRootID {
		t.Fatal("relay bound to unrelated destination profile", target)
	}
}
