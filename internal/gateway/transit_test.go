package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"quiclab/internal/protocol"
	"testing"
	"time"
)

type pipeStream struct{ net.Conn }

func (p pipeStream) CloseWrite() error { return p.Close() }
func TestTransitAndLocalEchoAreIndependent(t *testing.T) {
	for _, reachable := range []bool{false, true} {
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		s, _ := New("0.0.0.0/0", log)
		called := false
		s.Probe = func(context.Context) error {
			called = true
			if !reachable {
				return errors.New("upstream down")
			}
			return nil
		}
		for _, kind := range []string{"echo", "transit-probe"} {
			server, client := net.Pipe()
			ctx, cancel := context.WithCancel(context.Background())
			go s.handle(ctx, pipeStream{server}, "session", log)
			client.SetDeadline(time.Now().Add(time.Second))
			if e := WriteJSON(client, Request{Network: kind}); e != nil {
				t.Fatal(e)
			}
			var reply Reply
			if e := ReadJSON(client, &reply); e != nil {
				t.Fatal(e)
			}
			if kind == "echo" {
				if called {
					t.Fatal("local echo waited for transit")
				}
				json.NewEncoder(client).Encode(protocol.Frame{})
				var frame protocol.Frame
				if e := json.NewDecoder(client).Decode(&frame); e != nil {
					t.Fatal(e)
				}
			} else if !called || (reply.Error == "") != reachable {
				t.Fatal("incorrect transit probe result")
			}
			client.Close()
			cancel()
		}
	}
}
func TestTransitDialFailureDoesNotFallBack(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, _ := New("0.0.0.0/0", log)
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	s.DialContext = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("transit unavailable") }
	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.handle(ctx, pipeStream{server}, "session", log)
	client.SetDeadline(time.Now().Add(time.Second))
	WriteJSON(client, Request{Network: "tcp", Address: listener.Addr().String()})
	var reply Reply
	if e = ReadJSON(client, &reply); e != nil || reply.Error == "" {
		t.Fatal("direct fallback", e)
	}
}
