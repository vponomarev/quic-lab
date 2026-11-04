package configtransfer

import (
	"encoding/json"
	"quiclab/internal/mdm"
)

// Profile IDs belong to the source document, even if the same ID exists locally.
func remapProfileReferences(settings map[string]json.RawMessage, mapping map[string]string) error {
	if raw, ok := settings[mdm.AutomaticGroupMembers]; ok {
		var ids []string
		if json.Unmarshal(raw, &ids) != nil || ids == nil {
			return ErrInvalid
		}
		for i, id := range ids {
			target, ok := mapping[id]
			if !ok {
				return ErrInvalid
			}
			ids[i] = target
		}
		settings[mdm.AutomaticGroupMembers], _ = json.Marshal(ids)
	}
	if raw, ok := settings["relay_exit_profile_id"]; ok {
		var id string
		if json.Unmarshal(raw, &id) != nil {
			return ErrInvalid
		}
		if id != "" {
			target, ok := mapping[id]
			if !ok {
				return ErrInvalid
			}
			settings["relay_exit_profile_id"], _ = json.Marshal(target)
		}
	}
	return nil
}
