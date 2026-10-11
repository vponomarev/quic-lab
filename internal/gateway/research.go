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
	Session    string            `json:"session"`
	Generation uint64            `json:"generation,string"`
	Enabled    bool              `json:"enabled"`
	Records    []research.Record `json:"records,omitempty"`
}
type ResearchReply struct {
	Allowed  bool `json:"allowed"`
	Accepted int  `json:"accepted"`
}
type researchSession struct {
	generation uint64
	enabled    bool
}
type ResearchReceiver struct {
	mu           sync.Mutex
	identity     func(tls.ConnectionState) (string, string, bool)
	appendRecord func(research.Record) bool
	sessions     map[string]researchSession
	devices      map[string]int
}

func NewResearchReceiver(identity func(tls.ConnectionState) (string, string, bool), appendRecord func(research.Record) bool) *ResearchReceiver {
	return &ResearchReceiver{identity: identity, appendRecord: appendRecord, sessions: map[string]researchSession{}, devices: map[string]int{}}
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
	key := device + "/" + b.Session
	state, exists := r.sessions[key]
	if !exists {
		// Never evict a revocation tombstone while old authenticated streams can retry.
		// Exhaustion fails closed for diagnostics, without affecting VPN data.
		if len(r.sessions) >= 4096 || r.devices[device] >= 128 {
			return ResearchReply{}
		}
		r.devices[device]++
	} else if b.Generation < state.generation || (b.Generation == state.generation && b.Enabled != state.enabled) {
		return ResearchReply{}
	}
	r.sessions[key] = researchSession{b.Generation, b.Enabled}
	reply := ResearchReply{Allowed: allowed && b.Enabled}
	if !reply.Allowed {
		return reply
	}
	for _, record := range b.Records {
		if ctx.Err() != nil {
			break
		}
		if record.Generation != b.Generation {
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
		data["user_id"], _ = json.Marshal(user)
		record.Data, err = json.Marshal(data)
		if err != nil {
			continue
		}
		if err = research.EncodeRecord(new(bytes.Buffer), record); err != nil {
			continue
		}
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
