package configtransfer

import (
	"bytes"
	"encoding/json"
	"io"
	"quiclab/internal/mdm"
	"unicode/utf8"
)

type ImportOptions struct {
	Mode    string            `json:"mode"`
	Mapping map[string]string `json:"mapping"`
}
type Preview struct {
	Candidate          json.RawMessage `json:"candidate"`
	MissingCredentials []string        `json:"missingCredentials"`
	Sections           []string        `json:"sections"`
}

func strict(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > MaxPlaintext || !utf8.Valid(raw) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if walk(d, 0) != nil {
		return ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return ErrInvalid
	}
	return nil
}
func walk(d *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrInvalid
	}
	t, err := d.Token()
	if err != nil || t == nil {
		return ErrInvalid
	}
	switch t {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			s, ok := k.(string)
			if e != nil || !ok || seen[s] {
				return ErrInvalid
			}
			seen[s] = true
			if walk(d, depth+1) != nil {
				return ErrInvalid
			}
		}
		t, err = d.Token()
		if err != nil || t != json.Delim('}') {
			return ErrInvalid
		}
	case json.Delim('['):
		for d.More() {
			if walk(d, depth+1) != nil {
				return ErrInvalid
			}
		}
		t, err = d.Token()
		if err != nil || t != json.Delim(']') {
			return ErrInvalid
		}
	}
	return nil
}
func Prepare(base, raw []byte, o ImportOptions) (Preview, error) {
	fail := func() (Preview, error) { return Preview{}, ErrInvalid }
	if mdm.ValidateDocument(base) != nil {
		return fail()
	}
	var b Bundle
	if strict(raw, &b) != nil || b.Format != "quic-lab-config" || b.Version != 1 || (b.Credentials != "omitted" && b.Credentials != "included") || len(b.Payload) == 0 {
		return fail()
	}
	if o.Mode != "add" && o.Mode != "update" && o.Mode != "replace" {
		return fail()
	}
	for k := range b.Payload {
		switch k {
		case "profiles", "routing", "network", "diagnostics":
		default:
			return fail()
		}
	}
	var d mdm.ConfigurationDocument
	json.Unmarshal(base, &d)
	existing := map[string]int{}
	for i, p := range d.Profiles {
		existing[p.ID] = i
	}
	if o.Mode == "replace" {
		for _, k := range []string{"profiles", "routing", "network", "diagnostics"} {
			if b.Payload[k] == nil {
				return fail()
			}
		}
		d.Profiles = nil
		d.EnabledProfileIDs = []string{}
		d.Multiple = false
	}
	result := Preview{MissingCredentials: []string{}, Sections: []string{}}
	targets := map[string]bool{}
	mapped := map[string]string{}
	for from, to := range o.Mapping {
		if from == "" || to == "" || targets[to] {
			return fail()
		}
		targets[to] = true
		mapped[from] = to
	}
	if r, ok := b.Payload["profiles"]; ok {
		var ps []mdm.ConfigurationProfile
		if strict(r, &ps) != nil || len(ps) == 0 {
			return fail()
		}
		seen := map[string]bool{}
		for _, p := range ps {
			if seen[p.ID] || p.Settings == nil || p.RemoveIdentity {
				return fail()
			}
			seen[p.ID] = true
			if b.Credentials == "omitted" && p.Identity != nil {
				return fail()
			}
			id, ok := mapped[p.ID]
			if !ok {
				return fail()
			}
			oldIndex, exists := existing[id]
			if (o.Mode == "add" || o.Mode == "replace") && exists || o.Mode == "update" && !exists {
				return fail()
			}
			// Routing settings belong to their own independently selected section.
			for k := range p.Settings {
				if routingKey(k) {
					return fail()
				}
			}
			p.ID = id
			if o.Mode == "update" {
				old := d.Profiles[oldIndex]
				for k, v := range old.Settings {
					if routingKey(k) {
						p.Settings[k] = v
					}
				}
				if p.Identity == nil {
					p.Identity = old.Identity
				}
				d.Profiles[oldIndex] = p
			} else {
				d.Profiles = append(d.Profiles, p)
				if p.Identity == nil {
					result.MissingCredentials = append(result.MissingCredentials, id)
				}
			}
		}
	}
	remap := func(id string) (string, bool) {
		v, ok := mapped[id]
		if !ok {
			return "", false
		}
		for _, p := range d.Profiles {
			if p.ID == v {
				return v, true
			}
		}
		return "", false
	}
	if r, ok := b.Payload["routing"]; ok {
		var v Routing
		if !required(r, "currentProfileId", "enabledProfileIds", "multiple", "globalApps", "settings") || strict(r, &v) != nil || v.Settings == nil || v.EnabledProfileIDs == nil || v.GlobalApps == nil {
			return fail()
		}
		current, ok := remap(v.CurrentProfileID)
		if !ok {
			return fail()
		}
		enabled := []string{}
		for _, id := range v.EnabledProfileIDs {
			to, ok := remap(id)
			if !ok {
				return fail()
			}
			enabled = append(enabled, to)
		}
		for id, settings := range v.Settings {
			to, ok := remap(id)
			if !ok {
				return fail()
			}
			for _, p := range d.Profiles {
				if p.ID != to {
					continue
				}
				for k := range p.Settings {
					if routingKey(k) {
						delete(p.Settings, k)
					}
				}
				for k, value := range settings {
					if !routingKey(k) {
						return fail()
					}
					p.Settings[k] = value
				}
			}
		}
		d.GlobalApps = v.GlobalApps
		if o.Mode != "add" {
			d.CurrentProfileID = current
			d.EnabledProfileIDs = enabled
			d.Multiple = v.Multiple
		}
	}
	if r, ok := b.Payload["network"]; ok {
		var v Network
		if !required(r, "dns", "budget") || strict(r, &v) != nil {
			return fail()
		}
		id, ok := remap(v.DNS.ProfileID)
		if !ok {
			return fail()
		}
		v.DNS.ProfileID = id
		d.DNS = v.DNS
		d.Budget = v.Budget
		d.Reserve = v.Reserve
	}
	if r, ok := b.Payload["diagnostics"]; ok {
		if !required(r, "enabled", "detailed") || strict(r, &d.Diagnostics) != nil {
			return fail()
		}
	}
	// New incomplete profiles never become enabled through an imported routing list.
	for _, missing := range result.MissingCredentials {
		enabled := []string{}
		for _, id := range d.EnabledProfileIDs {
			if id != missing {
				enabled = append(enabled, id)
			}
		}
		d.EnabledProfileIDs = enabled
	}
	if len(d.EnabledProfileIDs) == 0 {
		d.Multiple = false
	}
	out, err := json.Marshal(d)
	if err != nil || mdm.ValidateDocument(out) != nil {
		return fail()
	}
	result.Candidate = out
	for _, s := range []string{"profiles", "routing", "network", "diagnostics"} {
		if b.Payload[s] != nil {
			result.Sections = append(result.Sections, s)
		}
	}
	return result, nil
}

func required(raw []byte, keys ...string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	for _, k := range keys {
		if m[k] == nil {
			return false
		}
	}
	if m["dns"] != nil && !required(m["dns"], "mode", "profileId") {
		return false
	}
	if m["budget"] != nil && !required(m["budget"], "limitBytes") {
		return false
	}
	return true
}

// DecodeOptions applies the same strict JSON rules to bridge inputs.
func DecodeOptions(raw []byte, out any) error { return strict(raw, out) }
