package mdm

import (
	"encoding/json"
	"time"
)

func (s *Store) Activate(id string, now time.Time) (Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Bindings[id]
	if !ok {
		return Binding{}, ErrUnauthorized
	}
	if len(b.Audit) >= 10000 {
		return Binding{}, ErrLimit
	}
	n := s.clone()
	b = n.Bindings[id]
	b.Binding.Epoch++
	b.Binding.Active = true
	b.Commands = nil
	b.Audit = append(b.Audit, auditEntry{Event: Event{ID: randomID(), Actor: "device", Kind: "activate", OccurredAt: now}, ReceivedAt: now})
	n.Bindings[id] = b
	return b.Binding, s.save(n)
}
func (s *Store) Pause(id string, epoch int64, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Bindings[id]
	if !ok {
		return ErrUnauthorized
	}
	if b.Binding.Epoch != epoch {
		return ErrConflict
	}
	n := s.clone()
	b = n.Bindings[id]
	b.Binding.Active = false
	b.Commands = nil
	if len(b.Audit) < 10000 {
		b.Audit = append(b.Audit, auditEntry{Event: Event{ID: randomID(), Actor: "device", Kind: "pause", OccurredAt: now}, ReceivedAt: now})
	}
	n.Bindings[id] = b
	return s.save(n)
}
func (s *Store) SetDesired(id string, expected int64, mode string, document json.RawMessage) (ConfigRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if (mode != "current" && mode != "external") || ValidateDocument(document) != nil {
		return ConfigRevision{}, ErrInvalid
	}
	b, ok := s.state.Bindings[id]
	if !ok {
		return ConfigRevision{}, ErrUnauthorized
	}
	revision := int64(0)
	if b.Desired != nil {
		revision = b.Desired.Revision
	}
	if revision != expected {
		return ConfigRevision{}, ErrConflict
	}
	if len(b.Audit) >= 10000 {
		return ConfigRevision{}, ErrLimit
	}
	n := s.clone()
	b = n.Bindings[id]
	r := ConfigRevision{Revision: revision + 1, Mode: mode, Document: append(json.RawMessage(nil), document...)}
	b.Desired = &r
	b.Audit = append(b.Audit, auditEntry{Event: Event{ID: randomID(), Actor: "admin", Kind: "config_update", Revision: r.Revision, OccurredAt: time.Now().UTC()}, ReceivedAt: time.Now().UTC()})
	n.Bindings[id] = b
	if e := s.save(n); e != nil {
		return ConfigRevision{}, e
	}
	// Return an independent copy, never mutable persisted state.
	r.Document = append(json.RawMessage(nil), r.Document...)
	return r, nil
}
func (s *Store) QueueVPN(id, kind string, now time.Time) (Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind != "vpn_start" && kind != "vpn_stop" {
		return Command{}, ErrInvalid
	}
	b, ok := s.state.Bindings[id]
	if !ok {
		return Command{}, ErrUnauthorized
	}
	if !b.Binding.Active {
		return Command{}, ErrPaused
	}
	n := s.clone()
	b = n.Bindings[id]
	b.Commands = liveCommands(b.Commands, now)
	if len(b.Audit) >= 10000 {
		return Command{}, ErrLimit
	}
	if len(b.Commands) >= 100 {
		return Command{}, ErrLimit
	}
	c := Command{ID: randomID(), BindingID: id, Epoch: b.Binding.Epoch, IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute), Kind: kind}
	b.Commands = append(b.Commands, c)
	b.Audit = append(b.Audit, auditEntry{Event: Event{ID: randomID(), Actor: "admin", Kind: kind, CommandID: c.ID, OccurredAt: now}, ReceivedAt: now})
	n.Bindings[id] = b
	return c, s.save(n)
}
func validEvent(e Event) bool {
	if e.ID == "" || len(e.ID) > 128 || len(e.CommandID) > 128 || len(e.Result) > 128 || e.Revision < 0 {
		return false
	}
	if e.Actor != "user" && e.Actor != "device" {
		return false
	}
	switch e.Kind {
	case "vpn_start", "vpn_stop", "command_result", "config_result", "config_vpn_result", "rights_changed", "gap":
		return true
	}
	return false
}
func (s *Store) Sync(id string, req SyncRequest, now time.Time) (SyncResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Version != 1 || req.AppliedRevision < 0 || len(req.Events) > 100 || req.GrantedRights != nil && !validRights(*req.GrantedRights) {
		return SyncResponse{}, ErrInvalid
	}
	for _, v := range req.Events {
		if !validEvent(v) {
			return SyncResponse{}, ErrInvalid
		}
	}
	b, ok := s.state.Bindings[id]
	if !ok {
		return SyncResponse{}, ErrUnauthorized
	}
	if req.Epoch != b.Binding.Epoch {
		return SyncResponse{}, ErrConflict
	}
	if !b.Binding.Active {
		return SyncResponse{}, ErrPaused
	}
	if b.Desired == nil && req.AppliedRevision != 0 || b.Desired != nil && req.AppliedRevision > b.Desired.Revision {
		return SyncResponse{}, ErrConflict
	}
	n := s.clone()
	b = n.Bindings[id]
	changed := false
	if now.Sub(b.ReceivedAt) >= 30*time.Second {
		b.ReceivedAt = now
		changed = true
	}
	if req.GrantedRights != nil && b.Binding.GrantedRights != *req.GrantedRights {
		b.Binding.GrantedRights = *req.GrantedRights
		if len(b.Audit) < 10000 {
			raw, _ := json.Marshal(*req.GrantedRights)
			b.Audit = append(b.Audit, auditEntry{Event: Event{ID: randomID(), Actor: "device", Kind: "rights_changed", Result: string(raw), OccurredAt: now}, ReceivedAt: now})
		}
		changed = true
	}
	for _, v := range req.Events {
		if _, seen := b.Seen[v.ID]; seen {
			continue
		}
		if len(b.Audit) >= 10000 {
			return SyncResponse{}, ErrLimit
		}
		b.Seen[v.ID] = now
		b.Audit = append(b.Audit, auditEntry{Event: v, ReceivedAt: now})
		changed = true
		if v.Kind == "command_result" {
			pending := b.Commands[:0]
			for _, c := range b.Commands {
				if c.ID != v.CommandID {
					pending = append(pending, c)
				}
			}
			b.Commands = pending
		}
	}
	if changed {
		n.Bindings[id] = b
		if e := s.save(n); e != nil {
			return SyncResponse{}, e
		}
	}
	out := SyncResponse{Version: 1, Epoch: b.Binding.Epoch, Commands: liveCommands(b.Commands, now), ServerTime: now, Telemetry: effectiveTelemetry(b.Telemetry)}
	if b.Desired != nil && b.Desired.Revision > req.AppliedRevision {
		r := *b.Desired
		r.Document = append(json.RawMessage(nil), r.Document...)
		out.DesiredConfig = &r
	}
	return out, nil
}
