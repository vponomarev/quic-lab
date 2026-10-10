package gateway

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"quiclab/internal/research"
	"testing"
	"time"
)

func TestResearchConsentGeneration(t *testing.T) {
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "user", "device", true }, func(research.Record) bool { return true })
	ctx := context.Background()
	cs := tls.ConnectionState{}
	if !r.Receive(ctx, cs, ResearchBatch{Session: "0123456789abcdef0123456789abcdef", Generation: 2, Enabled: true}).Allowed {
		t.Fatal("grant")
	}
	if r.Receive(ctx, cs, ResearchBatch{Session: "0123456789abcdef0123456789abcdef", Generation: 3, Enabled: false}).Allowed {
		t.Fatal("revoke")
	}
	if r.Receive(ctx, cs, ResearchBatch{Session: "0123456789abcdef0123456789abcdef", Generation: 2, Enabled: true}).Allowed {
		t.Fatal("stale grant")
	}
}
func TestResearchReceiverOverridesIdentity(t *testing.T) {
	var got research.Record
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "user", "device", true }, func(v research.Record) bool { got = v; return true })
	v := research.Record{SchemaVersion: 1, Type: "gap", ID: "a", At: time.Now(), Generation: 1, Flow: research.FlowKey{DeviceID: "foreign", SessionID: "foreign"}, Data: []byte("{}")}
	r.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: "0123456789abcdef0123456789abcdef", Generation: 1, Enabled: true, Records: []research.Record{v}})
	if got.Flow.DeviceID != "device" || got.Flow.SessionID != "0123456789abcdef0123456789abcdef" {
		t.Fatal(got)
	}
}

type researchPipe struct{ net.Conn }

func (p researchPipe) CloseWrite() error { return nil }
func TestResearchWireAuthenticated(t *testing.T) {
	s, _ := New("0.0.0.0/0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Research = NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "user", "device", true }, func(research.Record) bool { return true })
	a, b := net.Pipe()
	defer a.Close()
	ctx := s.researchContext(context.Background(), tls.ConnectionState{})
	go s.handle(ctx, researchPipe{b}, "test", s.Log)
	stream, err := Open(context.Background(), func(context.Context) (Stream, error) { return researchPipe{a}, nil }, "research-v1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err = WriteJSON(stream, ResearchBatch{Session: "0123456789abcdef0123456789abcdef", Generation: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	var reply ResearchReply
	if err = ReadJSON(stream, &reply); err != nil || !reply.Allowed {
		t.Fatalf("%+v %v", reply, err)
	}
}
func TestResearchPolicyAndLimits(t *testing.T) {
	enabled := false
	calls := 0
	r := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "u", "d", enabled }, func(research.Record) bool { calls++; return true })
	b := ResearchBatch{Session: "0123456789abcdef0123456789abcdef", Generation: 1, Enabled: true}
	if r.Receive(context.Background(), tls.ConnectionState{}, b).Allowed {
		t.Fatal("server policy ignored")
	}
	enabled = true
	if !r.Receive(context.Background(), tls.ConnectionState{}, b).Allowed {
		t.Fatal("policy enable")
	}
	b.Records = make([]research.Record, 129)
	if r.Receive(context.Background(), tls.ConnectionState{}, b).Allowed {
		t.Fatal("record count bound")
	}
	b.Records = nil
	b.Session = "not a session"
	if r.Receive(context.Background(), tls.ConnectionState{}, b).Allowed {
		t.Fatal("invalid session")
	}
	if calls != 0 {
		t.Fatal("invalid append")
	}
}
