package mobile

import (
	"context"
	"encoding/json"
	"net"
	"quiclab/internal/gateway"
	"testing"
	"time"
)

type researchPipeStream struct{ net.Conn }

func (s researchPipeStream) CloseWrite() error { return nil }
func TestResearchWireHandshakeAndBatch(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	done := make(chan error, 1)
	batch := gateway.ResearchBatch{Session: "0123456789abcdef0123456789abcdef", Generation: 3, Enabled: true}
	go func() {
		var req gateway.Request
		if err := gateway.ReadJSON(right, &req); err != nil {
			done <- err
			return
		}
		if req.Network != "research-v1" || req.Address != "" {
			t.Error("wrong research control request")
		}
		if err := gateway.WriteJSON(right, gateway.Reply{}); err != nil {
			done <- err
			return
		}
		raw, err := gateway.ReadPacket(right, 65535)
		if err != nil {
			done <- err
			return
		}
		var got gateway.ResearchBatch
		err = json.Unmarshal(raw, &got)
		if err == nil && (got.Session != batch.Session || got.Generation != batch.Generation || !got.Enabled) {
			t.Error("batch altered")
		}
		reply, _ := json.Marshal(gateway.ResearchReply{Allowed: true})
		if err == nil {
			err = gateway.WritePacket(right, reply)
		}
		done <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reply, err := sendResearchBatch(ctx, func(context.Context) (gateway.Stream, error) { return researchPipeStream{left}, nil }, batch)
	if err != nil || !reply.Allowed {
		t.Fatalf("research exchange: %+v %v", reply, err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
func TestResearchUnsupportedServerFailsOnlyControlStream(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	go func() {
		var req gateway.Request
		gateway.ReadJSON(right, &req)
		gateway.WriteJSON(right, gateway.Reply{Error: "unsupported network"})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := sendResearchBatch(ctx, func(context.Context) (gateway.Stream, error) { return researchPipeStream{left}, nil }, gateway.ResearchBatch{})
	if err == nil {
		t.Fatal("old server must not grant research")
	}
}
