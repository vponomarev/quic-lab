package awgserver

import (
	"testing"
	"time"
)

func TestConfirmedActivityIgnoresServerSends(t *testing.T) {
	now := time.Now()
	old := now.Add(-time.Minute)
	p := PeerStatus{ID: "a", TX: 100, RX: 200, Handshake: old, Activity: old}
	v := p
	v.RX += 1000
	if got := confirmedActivity(p, v, now); !got.Equal(old) {
		t.Fatal("server send revived peer")
	}
	v.TX++
	if got := confirmedActivity(p, v, now); !got.Equal(now) {
		t.Fatal("client data ignored")
	}
	v = p
	v.Handshake = now
	if !confirmedActivity(p, v, now).Equal(now) {
		t.Fatal("handshake ignored")
	}
	if !confirmedActivity(PeerStatus{}, p, now).Equal(old) {
		t.Fatal("initial counters revived peer")
	}
	v = p
	v.TX = 0
	if !confirmedActivity(p, v, now).Equal(old) {
		t.Fatal("counter reset revived peer")
	}
}
func TestHealthDefaults(t *testing.T) {
	c := Config{}
	if c.HealthInterval() != 25*time.Second || c.OfflineAfter() != 75*time.Second {
		t.Fatal("defaults")
	}
}
