package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestSelectedExitDenialCoversAllRequests(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, _ := New("0.0.0.0/0", log)
	s.Egress = func(tls.ConnectionState) (*Egress, error) { return nil, errors.New("denied") }
	s.DialContext = func(context.Context, string, string) (net.Conn, error) {
		t.Error("legacy fallback")
		return nil, errors.New("fallback")
	}
	for _, kind := range []string{"tcp", "dns", "udp-stream-v1", "udp-datagram-v1", "icmp-echo-v1", "icmp-echo-v2", "exit-ip", "transit-probe"} {
		t.Run(kind, func(t *testing.T) {
			server, client := net.Pipe()
			defer client.Close()
			client.SetDeadline(time.Now().Add(time.Second))
			go s.handle(s.captureContext(context.Background(), tls.ConnectionState{}), pipeStream{server}, "session", log)
			WriteJSON(client, Request{Network: kind, Address: "192.0.2.1:53"})
			var reply Reply
			if err := ReadJSON(client, &reply); err != nil || reply.Error != "server exit unavailable" {
				t.Fatal(reply, err)
			}
		})
	}
}

func TestConcurrentSelectedExitTCPIsolation(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, _ := New("0.0.0.0/0", log)
	s.Egress = func(cs tls.ConnectionState) (*Egress, error) {
		name := cs.ServerName
		return &Egress{Resolver: &net.Resolver{}, DialContext: func(context.Context, string, string) (net.Conn, error) {
			left, right := net.Pipe()
			go func() { defer right.Close(); io.Copy(right, io.LimitReader(&exitReader{name: name}, int64(len(name)))) }()
			return left, nil
		}}, nil
	}
	for _, name := range []string{"west", "east"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server, client := net.Pipe()
			defer client.Close()
			client.SetDeadline(time.Now().Add(time.Second))
			go s.handle(s.captureContext(context.Background(), tls.ConnectionState{ServerName: name}), pipeStream{server}, "session", log)
			WriteJSON(client, Request{Network: "tcp", Address: "192.0.2.1:443"})
			var reply Reply
			if err := ReadJSON(client, &reply); err != nil || reply.Error != "" {
				t.Fatal(reply, err)
			}
			b, err := io.ReadAll(client)
			if err != nil || string(b) != name {
				t.Fatal(string(b), err)
			}
		})
	}
}

type exitReader struct{ name string }

func TestSelectedExitRequiresOwnResolver(t *testing.T) {
	s, _ := New("0.0.0.0/0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Egress = func(tls.ConnectionState) (*Egress, error) {
		return &Egress{DialContext: (&net.Dialer{}).DialContext}, nil
	}
	if _, err := s.selectedEgress(s.captureContext(context.Background(), tls.ConnectionState{})); err == nil {
		t.Fatal("selected exit allowed system DNS fallback")
	}
}

func (r *exitReader) Read(b []byte) (int, error) {
	if r.name == "" {
		return 0, io.EOF
	}
	n := copy(b, r.name)
	r.name = r.name[n:]
	return n, nil
}
