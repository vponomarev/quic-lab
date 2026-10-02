package admin

import (
	"quiclab/internal/awgserver"
	"testing"
	"time"
)

func TestDeviceAWGStatisticsAggregate(t *testing.T) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s.ConfigureAWG(awgConfig())
	u, e := s.CreateWithProtocols("owner", []string{"awg"})
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.CreateDevice(u.ID, "second")
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	s.ObserveAWG(awgserver.Status{Updated: now, Started: now, Peers: []awgserver.PeerStatus{{ID: u.ID, TX: 10, Handshake: now, Activity: now}, {ID: d.ID, TX: 20, Handshake: now, Activity: now}}})
	s.ObserveAWG(awgserver.Status{Updated: now, Started: now, Peers: []awgserver.PeerStatus{{ID: u.ID, TX: 15, Handshake: now, Activity: now}, {ID: d.ID, TX: 27, Handshake: now, Activity: now}}})
	rows := s.List()
	if len(rows) != 1 || rows[0].Stats.TX != 12 || len(rows[0].Stats.Connections) != 2 {
		t.Fatalf("device AWG stats not aggregated: %+v", rows[0].Stats)
	}
}
