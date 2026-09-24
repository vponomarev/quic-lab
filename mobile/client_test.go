package mobile

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"quiclab/internal/echo"
	"quiclab/internal/labcert"
	"quiclab/internal/protocol"
)

type eventSink struct{ ch chan map[string]any }

func (s *eventSink) OnEvent(raw string) {
	var e map[string]any
	json.Unmarshal([]byte(raw), &e)
	s.ch <- e
}

func testServer(t *testing.T) (string, string) {
	t.Helper()
	cp, kp, err := labcert.Generate()
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(cp, kp)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := quic.ListenAddr("127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{protocol.ALPN}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); echo.Serve(ctx, ln, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	t.Cleanup(func() { cancel(); ln.Close(); <-done })
	return ln.Addr().String(), labcert.Fingerprint(cert)
}

func nextEcho(t *testing.T, sink *eventSink, predicate func(map[string]any) bool) map[string]any {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-sink.ch:
			if e["event"] == "echo" && predicate(e) {
				return e
			}
		case <-timer.C:
			t.Fatal("timed out waiting for echo")
			return nil
		}
	}
}

func TestMigrationKeepsConnectionAndStream(t *testing.T) {
	addr, pin := testServer(t)
	sink := &eventSink{ch: make(chan map[string]any, 2048)}
	c := NewClient(sink)
	defer c.Stop()
	if err := c.Start(addr, "", pin, 20, nil); err != nil {
		t.Fatal(err)
	}
	first := nextEcho(t, sink, func(map[string]any) bool { return true })
	previous := first
	for range 40 {
		if err := c.Migrate(nil); err != nil {
			t.Fatal(err)
		}
		next := nextEcho(t, sink, func(e map[string]any) bool { return e["peer"] != previous["peer"] })
		if next["connection_id"] != first["connection_id"] || next["stream_id"] != first["stream_id"] {
			t.Fatalf("new connection or stream: %v -> %v", first, next)
		}
		if next["seq"].(float64) <= previous["seq"].(float64) {
			t.Fatal("sequence did not advance")
		}
		t.Logf("same connection=%s stream=%v, peer %s -> %s", first["connection_id"], first["stream_id"], previous["peer"], next["peer"])
		previous = next
	}

}

func TestWrongPinRejected(t *testing.T) {
	addr, _ := testServer(t)
	c := NewClient(nil)
	defer c.Stop()
	if err := c.Start(addr, "", strings.Repeat("00", 32), 50, nil); err == nil {
		t.Fatal("wrong certificate accepted")
	}
}

type brokenBinder struct{}

func (brokenBinder) Bind(int64) error { return io.ErrClosedPipe }

func TestBindFailureDoesNotDropExistingConnection(t *testing.T) {
	addr, pin := testServer(t)
	sink := &eventSink{ch: make(chan map[string]any, 2048)}
	c := NewClient(sink)
	defer c.Stop()
	if err := c.Start(addr, "", pin, 20, nil); err != nil {
		t.Fatal(err)
	}
	first := nextEcho(t, sink, func(map[string]any) bool { return true })
	if err := c.Migrate(brokenBinder{}); err == nil {
		t.Fatal("expected bind failure")
	}
	next := nextEcho(t, sink, func(e map[string]any) bool { return e["seq"].(float64) > first["seq"].(float64) })
	if next["connection_id"] != first["connection_id"] {
		t.Fatal("connection changed")
	}
}
