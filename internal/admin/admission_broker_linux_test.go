//go:build linux

package admin

import (
	"context"
	"errors"
	"os"
	"quiclab/internal/awgserver"
	"quiclab/internal/serverexit"
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

func TestAWGNamedExitRequiresWorkerSupportAndAppliedPolicy(t *testing.T) {
	s, _, d, token := updateFixture(t)
	os.Chmod(s.admissionDirectory(), 0700)
	s.ConfigureServerExits(exitDefinitions())
	s.SetServerExitApply(func([]string) error { return nil })
	s.mu.Lock()
	u := s.state.Users[d.UserID]
	u.Protocols = []string{"awg"}
	s.state.Users[u.ID] = u
	s.mu.Unlock()
	grantExits(t, s, d.UserID, &serverexit.Policy{Allowed: []string{"west", "east"}, Default: "west"})
	stop, err := s.StartAWGAdmission(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	s.admission.mu.Lock()
	s.admission.ready = time.Time{}
	s.admission.mu.Unlock()
	if _, err := awgserver.RequestAdmission(s.admissionDirectory(), d.ID); err == nil {
		t.Fatal("old worker admitted named exit")
	}
	if _, err := awgserver.RequestExitAdmission(s.admissionDirectory(), d.ID); err != nil {
		t.Fatal(err)
	}
	s.SetServerExitApply(func([]string) error { return errors.New("worker unavailable") })
	v, _ := s.DeviceServerExits(d.ID, token)
	if _, err := s.SelectServerExit(d.ID, token, "east", v.Revision); err == nil {
		t.Fatal("failed apply acknowledged")
	}
	if _, err := awgserver.RequestExitAdmission(s.admissionDirectory(), d.ID); err == nil {
		t.Fatal("unapplied policy admitted")
	}
}
