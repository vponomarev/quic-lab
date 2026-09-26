package gateway

import (
	"io"
	"log/slog"
	"testing"
)

func TestDestinationPolicy(t *testing.T) {
	s, e := New("192.168.50.0/24,10.0.0.8/32", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	for a, want := range map[string]bool{"192.168.50.8:443": true, "10.0.0.8:22": true, "10.0.0.9:22": false, "example.org:443": false, "[::1]:80": false, "192.168.50.8:0": false, "192.168.50.8:65536": false, "127.0.0.1:80": false} {
		if got := s.Permitted(a); got != want {
			t.Errorf("%s: %v", a, got)
		}
	}
	if _, e := New("", s.Log); e == nil {
		t.Fatal("empty policy accepted")
	}
}
