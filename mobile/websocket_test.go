package mobile

import (
	"net"
	"os"
	"testing"
)

func TestWebSocketBindingFailure(t *testing.T) {
	c := NewWebSocketClient(nil)
	defer c.Stop()
	if err := c.Start("127.0.0.1:443", "localhost", 50, brokenBinder{}); err == nil {
		t.Fatal("binder failure ignored")
	}
	if c.conn != nil {
		t.Fatal("failed dial left a connection")
	}
}

// Opt-in integration test against the configured public HTTPS server. No trust bypass.
func TestWebSocketPublicPKI(t *testing.T) {
	hostname := os.Getenv("QUIC_LAB_WSS_HOST")
	if hostname == "" {
		t.Skip("set QUIC_LAB_WSS_HOST for public integration test")
	}
	ips, err := net.LookupIP(hostname)
	if err != nil {
		t.Fatal(err)
	}
	var addr string
	for _, ip := range ips {
		if ip.To4() != nil {
			addr = net.JoinHostPort(ip.String(), "443")
			break
		}
	}
	if addr == "" {
		t.Fatal("no IPv4 address")
	}
	sink := &eventSink{ch: make(chan map[string]any, 2048)}
	c := NewWebSocketClient(sink)
	defer c.Stop()
	previous := ""
	for range 2 {
		if err := c.Start(addr, hostname, 50, nil); err != nil {
			t.Fatal(err)
		}
		first := nextEcho(t, sink, func(map[string]any) bool { return true })
		identity := first["connection_id"].(string)
		if identity == "" || identity == previous {
			t.Fatal("reconnect did not get a new identity")
		}
		previous = identity
		nextEcho(t, sink, func(e map[string]any) bool { return e["seq"].(float64) >= 10 })
		t.Logf("HTTPS echo: connection=%s RTT=%.1fms", identity, first["rtt_ms"])
		c.Stop()
		for len(sink.ch) > 0 {
			<-sink.ch
		}
	}
}
