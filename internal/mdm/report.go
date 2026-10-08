package mdm

import (
	"encoding/json"
	"time"
)

type AppEntry struct {
	PackageID string `json:"packageId"`
	Label     string `json:"label"`
}
type AppInventory struct {
	Entries  []AppEntry `json:"entries,omitempty"`
	Hash     string     `json:"hash"`
	Complete bool       `json:"complete"`
}
type DeviceReport struct {
	Version          int             `json:"version"`
	Sequence         int64           `json:"sequence"`
	ConfigGeneration int64           `json:"configGeneration"`
	Capabilities     []string        `json:"capabilities"`
	Name             string          `json:"name"`
	AppVersion       string          `json:"appVersion"`
	VPNState         string          `json:"vpnState,omitempty"`
	AppliedRevision  int64           `json:"appliedRevision"`
	Configuration    json.RawMessage `json:"configuration,omitempty"`
	Inventory        *AppInventory   `json:"inventory,omitempty"`
}
type DeviceState struct {
	Binding    Binding         `json:"binding"`
	UserID     string          `json:"userId"`
	Report     *DeviceReport   `json:"report,omitempty"`
	ReceivedAt time.Time       `json:"receivedAt"`
	Desired    *ConfigRevision `json:"desired,omitempty"`
	Commands   []Command       `json:"commands"`
	Events     []Event         `json:"events"`
}
type requestRecord struct {
	Hash    string
	Config  *ConfigRevision
	Command *Command
	At      time.Time
}

func supported(b bindingState) bool {
	if b.Report != nil {
		for _, c := range b.Report.Capabilities {
			if c == "web-control-v1" {
				return true
			}
		}
	}
	return false
}
func (s *Store) Report(id string, epoch int64, r DeviceReport, now time.Time) error {
	raw, e := json.Marshal(r)
	if e != nil || len(raw) > 1<<20 || r.Version != 1 || r.Sequence < 1 || r.ConfigGeneration < 0 || r.AppliedRevision < 0 || !displayName(r.Name, 100) || !displayName(r.AppVersion, 100) || len(r.Capabilities) > 16 {
		return ErrInvalid
	}
	for _, c := range r.Capabilities {
		if !displayName(c, 64) {
			return ErrInvalid
		}
	}
	if r.VPNState != "" && !oneOf(r.VPNState, "stopped", "starting", "connected", "recovering", "error") {
		return ErrInvalid
	}
	if len(r.Configuration) > 0 {
		if ValidateDocument(r.Configuration) != nil {
			return ErrInvalid
		}
		var d ConfigurationDocument
		_ = json.Unmarshal(r.Configuration, &d)
		for _, p := range d.Profiles {
			if p.Identity != nil || p.RemoveIdentity {
				return ErrInvalid
			}
		}
	}
	if r.Inventory != nil {
		if len(r.Inventory.Entries) > 2048 || !displayName(r.Inventory.Hash, 128) {
			return ErrInvalid
		}
		ids := []string{}
		for _, a := range r.Inventory.Entries {
			if !displayName(a.Label, 256) {
				return ErrInvalid
			}
			ids = append(ids, a.PackageID)
		}
		if !apps(ids) {
			return ErrInvalid
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Bindings[id]
	if !ok {
		return ErrUnauthorized
	}
	if b.Binding.Epoch != epoch {
		return ErrConflict
	}
	if !b.Binding.Active {
		return ErrPaused
	}
	if (len(r.Configuration) > 0 || r.Inventory != nil) && !b.Binding.GrantedRights.Config || r.VPNState != "" && !b.Binding.GrantedRights.VPN {
		return ErrUnauthorized
	}
	if b.Desired == nil && r.AppliedRevision != 0 || b.Desired != nil && r.AppliedRevision > b.Desired.Revision {
		return ErrConflict
	}
	hash := digest(string(raw))
	if b.Report != nil && r.Sequence <= b.Report.Sequence {
		if r.Sequence == b.Report.Sequence && hash == b.ReportHash {
			return nil
		}
		return ErrConflict
	}
	n := s.clone()
	b = n.Bindings[id]
	// A hash-only inventory is valid only against the last acknowledged full snapshot.
	if r.Inventory != nil && r.Inventory.Entries == nil {
		if b.Report == nil || b.Report.Inventory == nil || b.Report.Inventory.Hash != r.Inventory.Hash {
			return ErrConflict
		}
		r.Inventory = b.Report.Inventory
	}
	// Copy the caller-owned slices before persisting; save does not marshal back into n.
	copied, _ := json.Marshal(r)
	var own DeviceReport
	_ = json.Unmarshal(copied, &own)
	b.Report = &own
	b.ReportHash = hash
	b.ReceivedAt = now
	n.Bindings[id] = b
	return s.save(n)
}
func (s *Store) DeviceState(id string) (DeviceState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Bindings[id]
	if !ok {
		return DeviceState{}, ErrUnauthorized
	}
	b = s.clone().Bindings[id]
	out := DeviceState{Binding: b.Binding, UserID: b.UserID, Report: b.Report, ReceivedAt: b.ReceivedAt, Desired: b.Desired, Commands: b.Commands, Events: []Event{}}
	// Credential replacement is write-only in the ordinary admin read model.
	if out.Desired != nil {
		var doc ConfigurationDocument
		_ = json.Unmarshal(out.Desired.Document, &doc)
		for i := range doc.Profiles {
			doc.Profiles[i].Identity = nil
		}
		out.Desired.Document, _ = json.Marshal(doc)
	}
	start := len(b.Audit) - 200
	if start < 0 {
		start = 0
	}
	for _, a := range b.Audit[start:] {
		out.Events = append(out.Events, a.Event)
	}
	return out, nil
}
func (s *Store) SetDesiredChecked(id string, revision, generation int64, requestID, mode string, document json.RawMessage) (ConfigRevision, error) {
	if !displayName(requestID, 128) || generation < 0 || (mode != "current" && mode != "external") || ValidateDocument(document) != nil {
		return ConfigRevision{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Bindings[id]
	if !ok {
		return ConfigRevision{}, ErrUnauthorized
	}
	if !b.Binding.Active {
		return ConfigRevision{}, ErrPaused
	}
	if !b.Binding.GrantedRights.Config || !supported(b) {
		return ConfigRevision{}, ErrUnauthorized
	}
	raw, _ := json.Marshal([]any{revision, generation, mode, json.RawMessage(document)})
	hash := digest(string(raw))
	if old, ok := b.Requests[requestID]; ok {
		if old.Hash != hash || old.Config == nil {
			return ConfigRevision{}, ErrConflict
		}
		out := *old.Config
		out.Document = append(json.RawMessage(nil), out.Document...)
		return out, nil
	}
	current := int64(0)
	if b.Desired != nil {
		current = b.Desired.Revision
	}
	if revision != current || b.Report == nil || generation != b.Report.ConfigGeneration {
		return ConfigRevision{}, ErrConflict
	}
	if len(b.Requests) >= 1000 || len(b.Audit) >= 10000 {
		return ConfigRevision{}, ErrLimit
	}
	n := s.clone()
	b = n.Bindings[id]
	if b.Requests == nil {
		b.Requests = map[string]requestRecord{}
	}
	r := ConfigRevision{Revision: revision + 1, Mode: mode, Document: append(json.RawMessage(nil), document...), ExpectedGeneration: generation}
	now := time.Now().UTC()
	b.Desired = &r
	b.Requests[requestID] = requestRecord{Hash: hash, Config: &r, At: now}
	b.Audit = append(b.Audit, auditEntry{Event: Event{ID: randomID(), Actor: "admin", Kind: "config_update", Revision: r.Revision, OccurredAt: now}, ReceivedAt: now})
	n.Bindings[id] = b
	if e := s.save(n); e != nil {
		return ConfigRevision{}, e
	}
	r.Document = append(json.RawMessage(nil), r.Document...)
	return r, nil
}
func (s *Store) QueueVPNOnce(id, kind, requestID string, now time.Time) (Command, error) {
	if !displayName(requestID, 128) || (kind != "vpn_start" && kind != "vpn_stop") {
		return Command{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Bindings[id]
	if !ok {
		return Command{}, ErrUnauthorized
	}
	if !b.Binding.Active {
		return Command{}, ErrPaused
	}
	if !b.Binding.GrantedRights.VPN || !supported(b) {
		return Command{}, ErrUnauthorized
	}
	hash := digest(kind)
	if old, ok := b.Requests[requestID]; ok {
		if old.Hash != hash || old.Command == nil {
			return Command{}, ErrConflict
		}
		return *old.Command, nil
	}
	if len(b.Requests) >= 1000 || len(b.Audit) >= 10000 {
		return Command{}, ErrLimit
	}
	n := s.clone()
	b = n.Bindings[id]
	b.Commands = liveCommands(b.Commands, now)
	if len(b.Commands) >= 100 {
		return Command{}, ErrLimit
	}
	if b.Requests == nil {
		b.Requests = map[string]requestRecord{}
	}
	c := Command{ID: randomID(), BindingID: id, Epoch: b.Binding.Epoch, IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute), Kind: kind}
	b.Commands = append(b.Commands, c)
	b.Requests[requestID] = requestRecord{Hash: hash, Command: &c, At: now}
	b.Audit = append(b.Audit, auditEntry{Event: Event{ID: randomID(), Actor: "admin", Kind: kind, CommandID: c.ID, OccurredAt: now}, ReceivedAt: now})
	n.Bindings[id] = b
	return c, s.save(n)
}
