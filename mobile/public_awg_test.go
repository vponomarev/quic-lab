package mobile

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"quiclab/internal/awg"
	"quiclab/internal/echoawg"
	"testing"
	"time"
)

func TestPublicAWGEchoIsolated(t *testing.T) {
	pc, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ep := pc.LocalAddr().String()
	pc.Close()
	server, e := echoawg.Start(context.Background(), ep, func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Millisecond):
			return nil
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var cfg gatewayConfig
	if e = json.Unmarshal(w.Body.Bytes(), &cfg); e != nil {
		t.Fatal(e)
	}
	parsed, e := awg.Parse(cfg.AWGConfig)
	if e != nil {
		t.Fatal(e)
	}
	// The server, not AllowedIPs supplied to the client, must enforce isolation.
	parsed.Allowed = []string{"0.0.0.0/0"}
	engine, e := awg.Start(parsed, ep, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer engine.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e = awgPingProbe(ctx, engine, cfg.AWGProbeEndpoint, 1); e != nil {
		t.Fatal("local", e)
	}
	started := time.Now()
	if e = transitUDPProbe(ctx, engine, cfg.TransitEndpoint); e != nil {
		t.Fatal("transit", e)
	}
	if time.Since(started) < 30*time.Millisecond {
		t.Fatal("probe did not await transit")
	}
	for _, address := range []string{"127.0.0.1:22", "1.1.1.1:443", echoawg.Address + ":22"} {
		c, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		out, e := engine.DialContext(c, "tcp4", address)
		cancel()
		if e == nil {
			out.Close()
			t.Fatalf("unexpected access to %s", address)
		}
	}
	w = httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
}
