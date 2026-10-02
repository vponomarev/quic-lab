//go:build linux

package admin

import (
	"context"
	"os"
	"quiclab/internal/awgserver"
	"testing"
	"time"
)

func TestAWGAdmissionBrokerSharedLimit(t *testing.T) {
	s, u := enrollmentStore(t)
	s.admission = NewAdmission(1)
	if e := os.Chmod(s.admissionDirectory(), 0700); e != nil {
		t.Fatal(e)
	}
	// AWG policy is required, regardless of whether native credentials exist.
	s.mu.Lock()
	owner := s.state.Users[u.ID]
	owner.Protocols = []string{"awg", "quic"}
	s.state.Users[u.ID] = owner
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, e := s.StartAWGAdmission(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	if _, e := awgserver.RequestAdmission(s.admissionDirectory(), u.ID); e == nil {
		t.Fatal("startup quarantine bypass")
	}
	s.admission.mu.Lock()
	s.admission.ready = time.Time{}
	s.admission.mu.Unlock()
	ttl, e := awgserver.RequestAdmission(s.admissionDirectory(), u.ID)
	if e != nil || ttl != 2*time.Second {
		t.Fatal(ttl, e)
	}
	if _, e := s.admission.Acquire("another-device"); e == nil {
		t.Fatal("AWG broker lease not shared")
	}
	same, e := s.admission.Acquire(u.ID)
	if e != nil {
		t.Fatal(e)
	}
	same()
	if e := s.DisableDevice(u.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := awgserver.RequestAdmission(s.admissionDirectory(), u.ID); e == nil {
		t.Fatal("revoked device renewed")
	}
	if other, err := s.StartAWGAdmission(ctx); err == nil {
		other()
		t.Fatal("second broker replaced first")
	}
	stop()
	if _, e := awgserver.RequestAdmission(s.admissionDirectory(), u.ID); e == nil {
		t.Fatal("broker failure allowed traffic")
	}
	if info, e := os.Stat(s.admissionDirectory()); e != nil || info.Mode().Perm() != 0700 {
		t.Fatal("broker directory not private", e)
	}
}
