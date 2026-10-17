package mdm

import (
	"encoding/hex"
	"encoding/json"
	"net"
	"time"
)

type ProbeSource struct {
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
}
type ProbeRequest struct {
	SourceID    string `json:"sourceId"`
	Fingerprint string `json:"fingerprint"`
	Action      string `json:"action"`
	RunID       string `json:"runId,omitempty"`
}
type ProbeRow struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Endpoint      string     `json:"endpoint"`
	Outcome       string     `json:"outcome"`
	RelayOutcome  string     `json:"relayOutcome"`
	EchoSucceeded int        `json:"echoSucceeded"`
	Stage         string     `json:"stage"`
	Role          string     `json:"role"`
	TotalMS       int64      `json:"totalMs"`
	EchoRTTMS     *int64     `json:"echoRttMs,omitempty"`
	MeasuredAt    *time.Time `json:"measuredAt,omitempty"`
}
type ProbeSnapshot struct {
	RunID       string     `json:"runId"`
	SourceID    string     `json:"sourceId"`
	Fingerprint string     `json:"fingerprint"`
	Sequence    int64      `json:"sequence"`
	Network     string     `json:"network"`
	Initiator   string     `json:"initiator"`
	State       string     `json:"state"`
	StartedAt   time.Time  `json:"startedAt"`
	FinishedAt  time.Time  `json:"finishedAt"`
	Rows        []ProbeRow `json:"rows"`
	ReceivedAt  time.Time  `json:"receivedAt"`
}

func probeAllowed(b bindingState) bool {
	if !b.Binding.Active || !b.Binding.GrantedRights.VPN || !b.Binding.GrantedRights.Telemetry || b.Report == nil {
		return false
	}
	for _, c := range b.Report.Capabilities {
		if c == "vless_probe_v1" {
			return true
		}
	}
	return false
}
func validProbeSource(x ProbeSource) bool {
	_, e := hex.DecodeString(x.Fingerprint)
	return displayName(x.ID, 128) && displayName(x.Name, 100) && len(x.Fingerprint) == 64 && e == nil
}
func probeSource(b bindingState, id, fp string) bool {
	if b.Report == nil {
		return false
	}
	for _, s := range b.Report.ProbeSources {
		if s.ID == id && s.Fingerprint == fp {
			return true
		}
	}
	return false
}
func (s *Store) QueueProbe(id, requestID string, r ProbeRequest, now time.Time) (Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.state.Bindings[id]
	if !ok || !probeAllowed(b) {
		return Command{}, ErrUnauthorized
	}
	if !displayName(requestID, 128) || !oneOf(r.Action, "start", "retry", "cancel") || !probeSource(b, r.SourceID, r.Fingerprint) {
		return Command{}, ErrInvalid
	}
	if r.Action != "start" && !displayName(r.RunID, 128) {
		return Command{}, ErrInvalid
	}
	raw, _ := json.Marshal(r)
	hash := digest(string(raw))
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
	c := Command{ID: randomID(), BindingID: id, Epoch: b.Binding.Epoch, IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute), Kind: "vless_probe", Probe: &r}
	if b.Requests == nil {
		b.Requests = map[string]requestRecord{}
	}
	b.Requests[requestID] = requestRecord{Hash: hash, Command: &c, At: now}
	b.Commands = append(b.Commands, c)
	b.Audit = append(b.Audit, auditEntry{Event: Event{ID: randomID(), Actor: "admin", Kind: "vless_probe", CommandID: c.ID, OccurredAt: now}, ReceivedAt: now})
	n.Bindings[id] = b
	if e := s.save(n); e != nil {
		return Command{}, e
	}
	return c, nil
}
func validProbe(p ProbeSnapshot) bool {
	raw, e := json.Marshal(p)
	if e != nil || len(raw) > 512*1024 || !displayName(p.RunID, 128) || p.Sequence < 1 || len(p.Rows) > 256 || !oneOf(p.Network, "wifi", "cell", "none") || !oneOf(p.Initiator, "local", "mdm") || !oneOf(p.State, "queued", "running", "finished", "interrupted", "canceled", "network_changed", "configuration_changed", "needs_foreground", "permission_denied") || p.StartedAt.IsZero() {
		return false
	}
	seen := map[string]bool{}
	for _, r := range p.Rows {
		if !displayName(r.ID, 128) || seen[r.ID] || !displayName(r.Name, 100) || len(r.Endpoint) > 300 {
			return false
		}
		seen[r.ID] = true
		if _, _, e := net.SplitHostPort(r.Endpoint); e != nil {
			return false
		}
		if !oneOf(r.Outcome, "queued", "running", "success", "relay_failed", "dns", "timeout", "transport", "target_tls", "unexpected_response", "budget", "network_changed", "canceled", "configuration_changed", "interrupted") || !oneOf(r.RelayOutcome, "", "not_applicable", "success", "relay_auth", "transport", "timeout", "canceled") || r.TotalMS < 0 || r.TotalMS > 2700000 || r.EchoSucceeded < 0 || r.EchoSucceeded > 3 || r.EchoRTTMS != nil && (*r.EchoRTTMS < 0 || *r.EchoRTTMS > 20000) || !oneOf(r.Role, "", "primary", "reserve") || !oneOf(r.Stage, "", "queued", "running", "finished") {
			return false
		}
	}
	return true
}
func mergeProbe(b *bindingState, p ProbeSnapshot, now time.Time) error {
	if !probeAllowed(*b) {
		return ErrUnauthorized
	}
	if !validProbe(p) || !probeSource(*b, p.SourceID, p.Fingerprint) {
		return ErrInvalid
	}
	if p.Initiator == "mdm" {
		found := false
		for _, record := range b.Requests {
			if record.Command != nil && record.Command.ID == p.RunID && record.Command.Kind == "vless_probe" && record.Command.Epoch == b.Binding.Epoch && record.Command.Probe.SourceID == p.SourceID {
				found = true
			}
		}
		if !found {
			return ErrConflict
		}
	}
	if old, ok := b.Probes[p.SourceID]; ok {
		if old.RunID == p.RunID && p.Sequence <= old.Sequence || old.RunID != p.RunID && !p.StartedAt.After(old.StartedAt) {
			return nil
		}
	}
	if b.Probes == nil {
		b.Probes = map[string]ProbeSnapshot{}
	}
	if _, ok := b.Probes[p.SourceID]; !ok && len(b.Probes) >= 32 {
		var key string
		var oldest time.Time
		for k, v := range b.Probes {
			if key == "" || v.ReceivedAt.Before(oldest) {
				key = k
				oldest = v.ReceivedAt
			}
		}
		delete(b.Probes, key)
	}
	p.ReceivedAt = now
	b.Probes[p.SourceID] = p
	return nil
}
func (s *Store) SaveProbe(id string, epoch int64, p ProbeSnapshot, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.clone()
	b, ok := n.Bindings[id]
	if !ok {
		return ErrUnauthorized
	}
	if b.Binding.Epoch != epoch {
		return ErrConflict
	}
	if e := mergeProbe(&b, p, now); e != nil {
		return e
	}
	n.Bindings[id] = b
	return s.save(n)
}

func interruptRemoteProbes(b *bindingState, now time.Time) {
	for id, p := range b.Probes {
		if p.Initiator != "mdm" || (p.State != "running" && p.State != "queued") {
			continue
		}
		p.State = "interrupted"
		p.FinishedAt = now
		for i := range p.Rows {
			if p.Rows[i].Outcome == "running" || p.Rows[i].Outcome == "queued" {
				p.Rows[i].Outcome = "interrupted"
			}
		}
		b.Probes[id] = p
	}
}
