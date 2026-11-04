package mdm

import "encoding/json"

const AutomaticGroupMembers = "automatic_group_members"
const AutomaticGroupsCapability = "automatic_groups_v1"

func groupMembers(p ConfigurationProfile) []string {
	var ids []string
	_ = json.Unmarshal(p.Settings[AutomaticGroupMembers], &ids)
	return ids
}

func validateGroupReferences(d ConfigurationDocument) error {
	profiles := map[string]ConfigurationProfile{}
	for _, p := range d.Profiles {
		profiles[p.ID] = p
	}
	for _, p := range d.Profiles {
		for _, id := range groupMembers(p) {
			member, ok := profiles[id]
			if !ok || id == p.ID || len(groupMembers(member)) > 0 {
				return ErrInvalid
			}
		}
	}
	if d.Multiple {
		owners := map[string]string{}
		for _, root := range d.EnabledProfileIDs {
			for _, id := range append([]string{root}, groupMembers(profiles[root])...) {
				if previous, ok := owners[id]; ok && previous != root {
					return ErrInvalid
				}
				owners[id] = root
			}
		}
	}
	return nil
}

func allowsAutomaticGroups(b bindingState, raw json.RawMessage) bool {
	var d ConfigurationDocument
	if json.Unmarshal(raw, &d) != nil {
		return false
	}
	needed := false
	for _, p := range d.Profiles {
		if _, ok := p.Settings[AutomaticGroupMembers]; ok {
			needed = true
		}
	}
	if !needed {
		return true
	}
	if b.Report != nil {
		for _, c := range b.Report.Capabilities {
			if c == AutomaticGroupsCapability {
				return true
			}
		}
	}
	return false
}
