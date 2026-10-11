package gateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"quiclab/internal/research"
	"sync"
	"time"
)

const MaxResearchBatchBytes = 65535

// ResearchBatch is carried only inside an authenticated own-tunnel stream.
type ResearchBatch struct {
	Historical    bool              `json:"historical,omitempty"`
	DirectEnabled bool              `json:"direct_enabled,omitempty"`
	Session       string            `json:"session"`
	Generation    uint64            `json:"generation,string"`
	Enabled       bool              `json:"enabled"`
	Records       []research.Record `json:"records,omitempty"`
}
type ResearchReply struct {
	DirectObservationVersion int  `json:"direct_observation_version,omitempty"`
	FlowObservationVersion   int  `json:"flow_observation_version,omitempty"`
	Allowed                  bool `json:"allowed"`
	Accepted                 int  `json:"accepted"`
}
type researchSession struct {
	historyUntil     time.Time
	historyDirect    bool
	directAuthorized bool
	directEnabled    bool
	allowed          bool
	until            time.Time
	generation       uint64
	enabled          bool
}
type ResearchReceiver struct {
	DirectAllowed                        func(user, device string) bool // Set before accepting connections.
	observationStates                    sync.Map                       // immutable researchSession snapshots; readers never wait for disk I/O
	identities                           map[[32]byte]observedIdentity
	apps                                 map[string]observedApp
	epoch                                string
	nextObservedFlow, nextObservedRecord uint64
	observers                            int
	mu                                   sync.Mutex
	identity                             func(tls.ConnectionState) (string, string, bool)
	appendRecord                         func(research.Record) bool
	sessions                             map[string]researchSession
	devices                              map[string]int
}

func NewResearchReceiver(identity func(tls.ConnectionState) (string, string, bool), appendRecord func(research.Record) bool) *ResearchReceiver {
	return &ResearchReceiver{epoch: identityNonce(), apps: make(map[string]observedApp), identities: make(map[[32]byte]observedIdentity), identity: identity, appendRecord: appendRecord, sessions: map[string]researchSession{}, devices: map[string]int{}}
}
func (r *ResearchReceiver) Receive(ctx context.Context, cs tls.ConnectionState, b ResearchBatch) ResearchReply {
	if r == nil || r.identity == nil || r.appendRecord == nil || ctx.Err() != nil || b.Generation == 0 || len(b.Session) != 32 || len(b.Records) > 128 {
		return ResearchReply{}
	}
	if _, err := hex.DecodeString(b.Session); err != nil {
		return ResearchReply{}
	}
	raw, err := json.Marshal(b)
	if err != nil || len(raw) > MaxResearchBatchBytes {
		return ResearchReply{}
	}
	user, device, allowed := r.identity(cs)
	if user == "" || device == "" {
		return ResearchReply{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Policy can change while another batch holds the consent lock.
	// Reauthenticate immediately before this batch's admission boundary.
	currentUser, currentDevice, currentAllowed := r.identity(cs)
	if currentUser != user || currentDevice != device {
		return ResearchReply{}
	}
	allowed = currentAllowed
	identityKey := observationIdentityKey(cs)
	if _, exists := r.identities[identityKey]; exists || len(r.identities) < 4096 {
		r.identities[identityKey] = observedIdentity{user, device}
	}
	key := device + "/" + b.Session
	state, exists := r.sessions[key]
	if !exists {
		// Never evict a revocation tombstone while old authenticated streams can retry.
		// Exhaustion fails closed for diagnostics, without affecting VPN data.
		if len(r.sessions) >= 4096 || r.devices[device] >= 128 {
			return ResearchReply{}
		}
		r.devices[device]++
	} else if b.Generation < state.generation || (b.Generation == state.generation && (b.Enabled != state.enabled || b.DirectEnabled != state.directEnabled)) {
		return ResearchReply{}
	}
	directAllowed := r.DirectAllowed == nil || r.DirectAllowed(user, device)
	historyUntil, historyDirect := state.historyUntil, state.historyDirect
	if exists && b.Generation > state.generation && state.enabled && state.allowed {
		historyUntil = time.Now().UTC()
		historyDirect = historyDirect || state.directAuthorized
	}
	r.sessions[key] = researchSession{directAuthorized: directAllowed && allowed && b.Enabled && b.DirectEnabled, historyUntil: historyUntil, historyDirect: historyDirect, directEnabled: b.DirectEnabled, generation: b.Generation, enabled: b.Enabled, allowed: allowed, until: time.Now().Add(15 * time.Second)}
	r.observationStates.Store(key, r.sessions[key])
	reply := ResearchReply{Allowed: allowed && b.Enabled, FlowObservationVersion: 1}
	if directAllowed {
		reply.DirectObservationVersion = 1
	}
	if !reply.Allowed && !(allowed && b.Historical && !historyUntil.IsZero()) {
		return reply
	}
	for _, record := range b.Records {
		if ctx.Err() != nil {
			break
		}
		historical := b.Historical && record.Generation < b.Generation && !historyUntil.IsZero() && !record.At.After(historyUntil)
		if !historical && (record.Generation != b.Generation || !reply.Allowed) {
			continue
		}
		switch record.Type {
		case "app", "flow_open", "flow_delta", "flow_close", "dns", "gap":
		default:
			continue
		}
		record.Origin = "client"
		record.ReceivedAt = time.Now().UTC().Format(time.RFC3339Nano)
		record.Flow.DeviceID = device
		record.Flow.SessionID = b.Session
		// Identity is authoritative for every record type, including gaps and DNS.
		var data map[string]json.RawMessage
		if json.Unmarshal(record.Data, &data) != nil || data == nil {
			continue
		}
		var exit research.ExitRef
		if json.Unmarshal(record.Data, &exit) != nil || (exit.Route == "direct" && (!b.DirectEnabled || !directAllowed) && !(historical && historyDirect)) {
			continue
		}
		delete(data, "app_source")
		delete(data, "client_flow")
		data["user_id"], _ = json.Marshal(user)
		record.Data, err = json.Marshal(data)
		if err != nil {
			continue
		}
		if err = research.EncodeRecord(new(bytes.Buffer), record); err != nil {
			continue
		}
		r.rememberAppLocked(record)
		if r.appendRecord(record) {
			reply.Accepted++
		}
	}
	return reply
}

type researchContextKey struct{}

func (s *Server) researchContext(ctx context.Context, cs tls.ConnectionState) context.Context {
	if s.Research == nil {
		return ctx
	}
	return context.WithValue(ctx, researchContextKey{}, cs)
}
func (s *Server) serveResearch(ctx context.Context, st Stream) {
	cs, ok := ctx.Value(researchContextKey{}).(tls.ConnectionState)
	if !ok || s.Research == nil {
		_ = WriteJSON(st, Reply{Error: "research unavailable"})
		return
	}
	if WriteJSON(st, Reply{}) != nil {
		return
	}
	_ = st.SetDeadline(time.Now().Add(5 * time.Second))
	raw, err := ReadPacket(st, MaxResearchBatchBytes)
	if err != nil {
		return
	}
	var b ResearchBatch
	if json.Unmarshal(raw, &b) != nil {
		return
	}
	reply := s.Research.Receive(ctx, cs, b)
	raw, err = json.Marshal(reply)
	if err == nil {
		_ = WritePacket(st, raw)
	}
}
