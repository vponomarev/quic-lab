package admin

import (
	"testing"
	"time"
)

func TestVLESSPeerDiagnosticsExpire(t *testing.T) {
	s := &Store{}
	now := time.Now()
	s.observeVLESSPeerLocked("user", "device", "198.51.100.7:12345", now, time.Second)
	s.observeVLESSPeerLocked("user", "device", "198.51.100.7:12346", now, time.Second)
	got := s.snapshot("user", now)
	if len(got.Connections) != 2 {
		t.Fatalf("connections=%d", len(got.Connections))
	}
	for _, c := range got.Connections {
		if c.Transport != "vless" || sourceIP(c.Source) == c.Source {
			t.Fatalf("source port lost: %+v", c)
		}
	}
	s.observeVLESSPeerLocked("user", "device", "198.51.100.7:12345", now.Add(time.Millisecond*500), time.Second)
	got = s.snapshot("user", now.Add(time.Millisecond*1100))
	if len(got.Connections) != 1 || got.Connections[0].Source != "198.51.100.7:12345" || !got.Connections[0].Connected.Equal(now) {
		t.Fatalf("renewed connection: %+v", got)
	}
	if got = s.snapshot("user", now.Add(2*time.Second)); len(got.Connections) != 0 {
		t.Fatal("stale source remains active")
	}
}

func TestVLESSPeerReusedPortStartsNewConnection(t *testing.T) {
	s := &Store{}
	now := time.Now()
	s.observeVLESSPeerLocked("user", "device", "198.51.100.7:12345", now, time.Second)
	later := now.Add(3 * time.Second)
	s.observeVLESSPeerLocked("user", "device", "198.51.100.7:12345", later, time.Second)
	got := s.snapshot("user", later)
	if len(got.Connections) != 1 || !got.Connections[0].Connected.Equal(later) {
		t.Fatalf("expired connection reused: %+v", got)
	}
}
