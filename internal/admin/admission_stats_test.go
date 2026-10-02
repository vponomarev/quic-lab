package admin

import (
	"quiclab/internal/awgserver"
	"testing"
	"time"
)

func TestAWGDeniedAdmissionNotReportedOnline(t *testing.T) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureAWG(awgConfig()); e != nil {
		t.Fatal(e)
	}
	u, e := s.CreateWithProtocols("phone", []string{"awg"})
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	status := awgserver.Status{Started: now, Updated: now, AdmissionEnforced: true, Peers: []awgserver.PeerStatus{{ID: u.ID, Activity: now, Handshake: now, Source: "127.0.0.1:1234"}}}
	s.ObserveAWG(status)
	if len(s.List()[0].Stats.Connections) != 0 {
		t.Fatal("unadmitted native handshake reported online")
	}
	status.Peers[0].Admitted = true
	s.ObserveAWG(status)
	if len(s.List()[0].Stats.Connections) != 1 {
		t.Fatal("admitted authenticated AWG peer not reported online")
	}
}
