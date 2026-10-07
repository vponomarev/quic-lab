package admin

import (
	"fmt"
	"testing"
	"time"
)

func TestDeviceCap(t *testing.T) {
	a := NewAdmission(30)
	var releases []func()
	for d := 0; d < 30; d++ {
		for p := 0; p < 5; p++ {
			release, e := a.Acquire(fmt.Sprint(d))
			if e != nil {
				t.Fatal(e)
			}
			releases = append(releases, release)
		}
	}
	if _, e := a.Acquire("31"); e == nil {
		t.Fatal("31st distinct device accepted")
	}
	existing, e := a.Acquire("0")
	if e != nil {
		t.Fatal("existing device refused", e)
	}
	existing()
	for _, release := range releases[:5] {
		release()
		release()
	}
	next, e := a.Acquire("31")
	if e != nil {
		t.Fatal("released device retained slot", e)
	}
	next()
	for _, release := range releases[5:] {
		release()
	}
}
func TestAWGAdmission(t *testing.T) {
	a := NewAdmission(1)
	quic, e := a.Acquire("phone")
	if e != nil {
		t.Fatal(e)
	}
	awg, e := a.Acquire("phone")
	if e != nil {
		t.Fatal("AWG counted same device twice", e)
	}
	quic()
	if _, e := a.Acquire("other"); e == nil {
		t.Fatal("QUIC release evicted active AWG lease")
	}
	awg()
	release, e := a.Acquire("other")
	if e != nil {
		t.Fatal(e)
	}
	release()
}
func TestAdmissionBrokerRestartQuarantine(t *testing.T) {
	now := time.Now()
	a := NewAdmission(1)
	a.now = func() time.Time { return now }
	a.Quarantine(3 * time.Second)
	if _, e := a.Acquire("new-quic"); e == nil {
		t.Fatal("new QUIC admitted before old AWG leases expire")
	}
	if e := a.Renew("new-awg", 2*time.Second); e == nil {
		t.Fatal("new AWG admitted during quarantine")
	}
	now = now.Add(3 * time.Second)
	if e := a.Renew("phone", 2*time.Second); e != nil {
		t.Fatal(e)
	}
	same, e := a.Acquire("phone")
	if e != nil {
		t.Fatal("same device counted twice", e)
	}
	same()
	if _, e := a.Acquire("other"); e == nil {
		t.Fatal("AWG lease ignored")
	}
	now = now.Add(2 * time.Second)
	release, e := a.Acquire("other")
	if e != nil {
		t.Fatal("expired AWG lease retained", e)
	}
	release()
}

func TestAdmissionStoreRegistrationAndRevoke(t *testing.T) {
	s, u := enrollmentStore(t)
	s.admission = NewAdmission(1)
	first, e := s.CreateDevice(u.ID, "first")
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.CreateDevice(u.ID, "second")
	if e != nil {
		t.Fatal(e)
	}
	release, e := s.RegisterProtocol(deviceTLS(t, first.Certificate), "quic", func() {})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.RegisterProtocol(deviceTLS(t, second.Certificate), "https", func() {}); e == nil {
		t.Fatal("HTTPS bypassed QUIC admission")
	}
	duplicate, e := s.Register(deviceTLS(t, first.Certificate), func() {})
	if e != nil {
		t.Fatal(e)
	}
	if e := s.DisableDevice(first.ID); e != nil {
		t.Fatal(e)
	}
	replacement, e := s.RegisterProtocol(deviceTLS(t, second.Certificate), "https", func() {})
	if e != nil {
		t.Fatal("revocation retained device slot", e)
	}
	release()
	duplicate()
	replacement()
	if s.admission.Count() != 0 {
		t.Fatal("admission cleanup not idempotent")
	}
}

func TestDefaultDeviceCapAtLeastThirty(t *testing.T) {
	a := NewAdmission(0)
	for d := 0; d < 30; d++ {
		if _, err := a.Acquire(fmt.Sprint(d)); err != nil {
			t.Fatalf("default rejected device %d: %v", d+1, err)
		}
	}
	if _, err := a.Acquire("31"); err == nil {
		t.Fatal("default must remain bounded at 30")
	}
}
