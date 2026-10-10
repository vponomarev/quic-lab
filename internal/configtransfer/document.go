package configtransfer

import (
	"encoding/json"
	"quiclab/internal/mdm"
)

type Selection struct {
	ProfileIDs []string `json:"profileIds"`
	Sections   []string `json:"sections"`
}
type Bundle struct {
	Format      string                     `json:"format"`
	Version     int                        `json:"version"`
	Credentials string                     `json:"credentials"`
	Payload     map[string]json.RawMessage `json:"payload"`
}
type Routing struct {
	CurrentProfileID  string                                `json:"currentProfileId"`
	EnabledProfileIDs []string                              `json:"enabledProfileIds"`
	Multiple          bool                                  `json:"multiple"`
	GlobalApps        []string                              `json:"globalApps"`
	Settings          map[string]map[string]json.RawMessage `json:"settings"`
}
type Network struct {
	DNS     mdm.ConfigurationDNS    `json:"dns"`
	Budget  mdm.ConfigurationBudget `json:"budget"`
	Reserve map[string]bool         `json:"reserve,omitempty"`
}

func routingKey(k string) bool {
	return k == "routes" || k == "apps" || k == "mode" || k == "global_apps"
}

// Export consumes only schema1; enrollment, management permissions and queues
// are not representable. Secret export must be encrypted before leaving caller.
func Export(raw []byte, selection Selection, includeSecrets bool) ([]byte, error) {
	if mdm.ValidateDocument(raw) != nil {
		return nil, ErrInvalid
	}
	var d mdm.ConfigurationDocument
	if json.Unmarshal(raw, &d) != nil {
		return nil, ErrInvalid
	}
	selected := map[string]bool{}
	for _, s := range selection.Sections {
		if selected[s] {
			return nil, ErrInvalid
		}
		switch s {
		case "profiles", "routing", "network", "diagnostics":
		default:
			return nil, ErrInvalid
		}
		selected[s] = true
	}
	if len(selected) == 0 {
		return nil, ErrInvalid
	}
	ids := map[string]bool{}
	for _, id := range selection.ProfileIDs {
		if ids[id] {
			return nil, ErrInvalid
		}
		ids[id] = true
	}
	if len(ids) > 0 && !selected["profiles"] {
		return nil, ErrInvalid
	}
	found := 0
	profiles := []mdm.ConfigurationProfile{}
	routing := Routing{d.CurrentProfileID, d.EnabledProfileIDs, d.Multiple, d.GlobalApps, map[string]map[string]json.RawMessage{}}
	for _, p := range d.Profiles {
		routes := map[string]json.RawMessage{}
		for k, v := range p.Settings {
			if routingKey(k) {
				routes[k] = v
				delete(p.Settings, k)
			}
		}
		routing.Settings[p.ID] = routes
		if len(ids) > 0 && !ids[p.ID] {
			continue
		}
		found++
		// Removing an identity is an edit action, never a transferable credential.
		p.RemoveIdentity = false
		if !includeSecrets {
			p.Identity = nil
		}
		profiles = append(profiles, p)
	}
	if len(ids) > 0 && found != len(ids) {
		return nil, ErrInvalid
	}
	b := Bundle{Format: "quic-lab-config", Version: 1, Credentials: "omitted", Payload: map[string]json.RawMessage{}}
	if includeSecrets {
		b.Credentials = "included"
	}
	put := func(k string, v any) { b.Payload[k], _ = json.Marshal(v) }
	if selected["profiles"] {
		put("profiles", profiles)
	}
	if selected["routing"] {
		put("routing", routing)
	}
	if selected["network"] {
		put("network", Network{d.DNS, d.Budget, d.Reserve})
	}
	if selected["diagnostics"] {
		put("diagnostics", d.Diagnostics)
	}
	out, err := json.Marshal(b)
	if err != nil || len(out) > MaxPlaintext {
		return nil, ErrInvalid
	}
	return out, nil
}
