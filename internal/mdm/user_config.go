package mdm

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"time"
	"unicode/utf8"
)

func strictJSONValue(raw []byte) error {
	if len(raw) == 0 || !utf8.Valid(raw) {
		return ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if uniqueValue(dec, 0) != nil {
		return ErrInvalid
	}
	if _, err := dec.Token(); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

const UserConfigCapability = "user_config_edit_v1"

// This is granted only by the administrator, never by device-reported Rights.
type UserConfigEditPolicy struct {
	Enabled  bool  `json:"enabled"`
	Revision int64 `json:"revision"`
}
type UserConfigView struct {
	DeviceState
	Editable bool `json:"editable"`
}
type UserConfigRequest struct {
	Version            int             `json:"version"`
	BindingID          string          `json:"bindingId"`
	Epoch              int64           `json:"epoch"`
	PolicyRevision     int64           `json:"policyRevision"`
	ExpectedRevision   int64           `json:"expectedRevision"`
	ExpectedGeneration int64           `json:"expectedGeneration"`
	RequestID          string          `json:"requestId"`
	Document           json.RawMessage `json:"document"`
}

func (s *Store) SetUserConfigEdit(id string, expected int64, enabled bool) (UserConfigEditPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Bindings[id]
	if !ok {
		return UserConfigEditPolicy{}, ErrUnauthorized
	}
	if expected < 0 || b.UserConfigEdit.Revision != expected {
		return UserConfigEditPolicy{}, ErrConflict
	}
	if b.UserConfigEdit.Enabled == enabled {
		return b.UserConfigEdit, nil
	}
	if expected == int64(^uint64(0)>>1) || len(b.Audit) >= 10000 {
		return UserConfigEditPolicy{}, ErrLimit
	}
	n := s.clone()
	b = n.Bindings[id]
	b.UserConfigEdit = UserConfigEditPolicy{Enabled: enabled, Revision: expected + 1}
	now := time.Now().UTC()
	b.Audit = append(b.Audit, auditEntry{Event: Event{ID: randomID(), Actor: "admin", Kind: "user_config_permission", Result: strconv.FormatBool(enabled), OccurredAt: now}, ReceivedAt: now})
	n.Bindings[id] = b
	if err := s.save(n); err != nil {
		return UserConfigEditPolicy{}, err
	}
	return b.UserConfigEdit, nil
}

func userConfigAccess(b bindingState, epoch int64) error {
	if b.Binding.Epoch != epoch {
		return ErrConflict
	}
	if !b.Binding.Active {
		return ErrPaused
	}
	if !b.Binding.GrantedRights.Config {
		return ErrUnauthorized
	}
	return nil
}
func userConfigReady(b bindingState) bool {
	return supported(b) && b.Report != nil && len(b.Report.Configuration) > 0 && (b.Desired == nil || b.Desired.Revision == b.Report.AppliedRevision)
}
func (s *Store) UserConfig(id string, epoch int64) (UserConfigView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Bindings[id]
	if !ok {
		return UserConfigView{}, ErrUnauthorized
	}
	if err := userConfigAccess(b, epoch); err != nil {
		return UserConfigView{}, err
	}
	state, err := s.deviceStateLocked(id)
	// Keep only editor data. In particular, do not send a second configuration
	// or potentially large probe history alongside the configuration/inventory.
	state.Probes = nil
	state.Commands = nil
	if state.Report != nil {
		r := state.Report
		if state.Desired != nil && r.AppliedRevision == state.Desired.Revision {
			// Removal is a one-shot command, not a setting for the next draft.
			var doc ConfigurationDocument
			if json.Unmarshal(state.Desired.Document, &doc) == nil {
				for i := range doc.Profiles {
					doc.Profiles[i].RemoveIdentity = false
				}
				state.Desired.Document, _ = json.Marshal(doc)
			}
		}
		state.Report = &DeviceReport{ConfigGeneration: r.ConfigGeneration, AppliedRevision: r.AppliedRevision,
			Capabilities: r.Capabilities, Inventory: r.Inventory, ResearchScope: r.ResearchScope}
		if state.Desired == nil {
			state.Report.Configuration = r.Configuration
		}
	}
	return UserConfigView{DeviceState: state, Editable: b.UserConfigEdit.Enabled && userConfigReady(b)}, err
}

func (s *Store) SubmitUserConfig(req UserConfigRequest) (ConfigRevision, error) {
	if req.Version != 1 || req.ExpectedRevision < 0 || req.ExpectedGeneration < 0 || req.PolicyRevision < 0 || !displayName(req.RequestID, 96) || len(req.Document) > 1<<20 {
		return ConfigRevision{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Bindings[req.BindingID]
	if !ok {
		return ConfigRevision{}, ErrUnauthorized
	}
	if err := userConfigAccess(b, req.Epoch); err != nil {
		return ConfigRevision{}, err
	}
	if !b.UserConfigEdit.Enabled {
		return ConfigRevision{}, ErrUnauthorized
	}
	if b.UserConfigEdit.Revision != req.PolicyRevision {
		return ConfigRevision{}, ErrConflict
	}
	// Hash the original request. Retry still works after policy-preserving
	// normalization and after the phone has applied and reported the revision.
	raw, _ := json.Marshal(req)
	hash := digest(string(raw))
	key := "device-config:" + req.RequestID
	if old, ok := b.Requests[key]; ok {
		if old.Hash != hash || old.Config == nil {
			return ConfigRevision{}, ErrConflict
		}
		out := *old.Config
		out.Document = append(json.RawMessage(nil), out.Document...)
		return out, nil
	}
	if !userConfigReady(b) {
		return ConfigRevision{}, ErrConflict
	}
	base := b.Report.Configuration
	mode := "current"
	if b.Desired != nil {
		base = b.Desired.Document
		mode = b.Desired.Mode
	}
	document, err := userConfigDocument(base, req.Document)
	if err != nil {
		return ConfigRevision{}, err
	}
	return s.setDesiredCheckedLocked(req.BindingID, req.ExpectedRevision, req.ExpectedGeneration, key, mode, document, "user", hash)
}

func userConfigDocument(base, submitted json.RawMessage) (json.RawMessage, error) {
	// Validate the submitted shape first, allowing only removal of profiles from
	// an unchanged server-owned selection policy. Duplicate/unknown keys still
	// pass through the common strict validator, never a lossy typed round trip.
	var previous ConfigurationDocument
	if json.Unmarshal(base, &previous) != nil {
		return nil, ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(submitted, &fields) != nil || fields == nil {
		return nil, ErrInvalid
	}
	var policy *ConfigurationProfileSelection
	if raw, ok := fields["profileSelection"]; ok && json.Unmarshal(raw, &policy) != nil {
		return nil, ErrInvalid
	}
	if !reflect.DeepEqual(previous.ProfileSelection, policy) {
		return nil, ErrUnauthorized
	}
	// Duplicate keys/nulls are rejected before reconstructing the document.
	if strictJSONValue(submitted) != nil {
		return nil, ErrInvalid
	}
	if policy != nil {
		var profiles []ConfigurationProfile
		if json.Unmarshal(fields["profiles"], &profiles) != nil {
			return nil, ErrInvalid
		}
		ids := map[string]bool{}
		for _, p := range profiles {
			ids[p.ID] = true
		}
		keep := []string{}
		for _, id := range policy.ProfileIDs {
			if ids[id] {
				keep = append(keep, id)
			}
		}
		policy.ProfileIDs = keep
		if len(keep) == 0 {
			policy.Enabled = false
		}
		fields["profileSelection"], _ = json.Marshal(policy)
	}
	normalized, _ := json.Marshal(fields)
	if ValidateDocument(normalized) != nil {
		return nil, ErrInvalid
	}
	return normalized, nil
}
