package awgserver

import (
	"errors"
	"github.com/amnezia-vpn/amneziawg-go/tun"
	"net/netip"
	"os"
	"testing"
	"time"
)

type admissionTestTUN struct {
	writes   int
	outgoing [][]byte
	events   chan tun.Event
}

func (f *admissionTestTUN) File() *os.File           { return nil }
func (f *admissionTestTUN) MTU() (int, error)        { return 1280, nil }
func (f *admissionTestTUN) Name() (string, error)    { return "test", nil }
func (f *admissionTestTUN) Events() <-chan tun.Event { return f.events }
func (f *admissionTestTUN) Close() error             { return nil }
func (f *admissionTestTUN) BatchSize() int           { return 16 }
func (f *admissionTestTUN) Write(bufs [][]byte, offset int) (int, error) {
	f.writes += len(bufs)
	return len(bufs), nil
}
func (f *admissionTestTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	for i, p := range f.outgoing {
		copy(bufs[i][offset:], p)
		sizes[i] = len(p)
	}
	n := len(f.outgoing)
	f.outgoing = nil
	return n, nil
}
func admissionPacket(src, dst string) []byte {
	p := make([]byte, 20)
	p[0] = 0x45
	p[2] = 0
	p[3] = 20
	s := netip.MustParseAddr(src).As4()
	d := netip.MustParseAddr(dst).As4()
	copy(p[12:16], s[:])
	copy(p[16:20], d[:])
	return p
}
func TestAWGAdmissionBeforeData(t *testing.T) {
	now := time.Now()
	fake := &admissionTestTUN{}
	allowed := false
	calls := 0
	gate := NewAdmissionTUN(fake, func(id string) (time.Duration, error) {
		calls++
		if !allowed {
			return 0, errors.New("full")
		}
		return 2 * time.Second, nil
	}, 75*time.Second)
	gate.now = func() time.Time { return now }
	gate.Update(map[netip.Addr]string{netip.MustParseAddr("10.77.0.2"): "phone"})
	packet := admissionPacket("10.77.0.2", "10.77.0.1")
	gate.Write([][]byte{packet}, 0)
	if fake.writes != 0 {
		t.Fatal("unadmitted authenticated packet forwarded")
	}
	for i := 0; i < 10; i++ {
		gate.Write([][]byte{packet}, 0)
	}
	if calls != 1 {
		t.Fatal("denied packets caused per-packet IPC")
	}
	now = now.Add(time.Second)
	allowed = true
	gate.Write([][]byte{packet}, 0)
	if fake.writes != 1 {
		t.Fatal("admitted packet not forwarded")
	}
	fake.outgoing = [][]byte{admissionPacket("10.77.0.1", "10.77.0.2")}
	bufs := [][]byte{make([]byte, 1280)}
	sizes := make([]int, 1)
	n, _ := gate.Read(bufs, sizes, 0)
	if n != 1 {
		t.Fatal("admitted outbound packet blocked")
	}
	now = now.Add(3 * time.Second)
	fake.outgoing = [][]byte{admissionPacket("10.77.0.1", "10.77.0.2")}
	n, _ = gate.Read(bufs, sizes, 0)
	if n != 0 {
		t.Fatal("expired outbound lease forwarded")
	}
	gate.Update(map[netip.Addr]string{})
	gate.Write([][]byte{packet}, 0)
	if fake.writes != 1 {
		t.Fatal("revoked peer forwarded")
	}
}
func TestAWGAdmissionDelayedResponse(t *testing.T) {
	now := time.Now()
	fake := &admissionTestTUN{}
	gate := NewAdmissionTUN(fake, func(string) (time.Duration, error) { now = now.Add(3 * time.Second); return 2 * time.Second, nil }, 75*time.Second)
	gate.now = func() time.Time { return now }
	gate.Update(map[netip.Addr]string{netip.MustParseAddr("10.77.0.2"): "phone"})
	gate.Write([][]byte{admissionPacket("10.77.0.2", "10.77.0.1")}, 0)
	if fake.writes != 0 {
		t.Fatal("slow response extended lease beyond broker reservation")
	}
}
func TestAWGLeaseFailsClosedOnRenewalFailureAndIdle(t *testing.T) {
	now := time.Now()
	fake := &admissionTestTUN{}
	available := true
	calls := 0
	gate := NewAdmissionTUN(fake, func(string) (time.Duration, error) {
		calls++
		if !available {
			return 0, errors.New("broker down")
		}
		return 2 * time.Second, nil
	}, 75*time.Second)
	gate.now = func() time.Time { return now }
	gate.Update(map[netip.Addr]string{netip.MustParseAddr("10.77.0.2"): "phone"})
	gate.Write([][]byte{admissionPacket("10.77.0.2", "10.77.0.1")}, 0)
	now = now.Add(time.Second)
	gate.Tick()
	if calls != 2 {
		t.Fatal("active authenticated lease was not renewed")
	}
	available = false
	now = now.Add(time.Second)
	gate.Tick()
	fake.outgoing = [][]byte{admissionPacket("10.77.0.1", "10.77.0.2")}
	n, _ := gate.Read([][]byte{make([]byte, 1280)}, make([]int, 1), 0)
	if n != 0 {
		t.Fatal("broker failure extended outbound lease")
	}
	now = now.Add(76 * time.Second)
	before := calls
	gate.Tick()
	if calls != before || len(gate.leases) != 0 {
		t.Fatal("confirmed idle timeout retained/renewed lease")
	}
}
func TestAWGBatchDelayedAdmissionCannotForwardExpiredLease(t *testing.T) {
	now := time.Now()
	fake := &admissionTestTUN{}
	calls := 0
	gate := NewAdmissionTUN(fake, func(string) (time.Duration, error) {
		calls++
		if calls == 2 {
			now = now.Add(3 * time.Second)
		}
		return 2 * time.Second, nil
	}, 75*time.Second)
	gate.now = func() time.Time { return now }
	gate.Update(map[netip.Addr]string{netip.MustParseAddr("10.77.0.2"): "first", netip.MustParseAddr("10.77.0.3"): "second"})
	gate.Write([][]byte{admissionPacket("10.77.0.2", "10.77.0.1"), admissionPacket("10.77.0.3", "10.77.0.1")}, 0)
	if fake.writes != 0 {
		t.Fatal("batch forwarded a lease expired while another admission was pending")
	}
}
func TestAWGConfirmedKeepaliveRenewsOnlyExistingAdmission(t *testing.T) {
	now := time.Now()
	fake := &admissionTestTUN{}
	calls := 0
	gate := NewAdmissionTUN(fake, func(string) (time.Duration, error) { calls++; return 2 * time.Second, nil }, 75*time.Second)
	gate.now = func() time.Time { return now }
	gate.Update(map[netip.Addr]string{netip.MustParseAddr("10.77.0.2"): "phone", netip.MustParseAddr("10.77.0.3"): "unadmitted"})
	gate.Write([][]byte{admissionPacket("10.77.0.2", "10.77.0.1")}, 0)
	for i := 0; i < 74; i++ {
		now = now.Add(time.Second)
		gate.Tick()
	}
	before := calls
	gate.ObserveActivity([]PeerStatus{{ID: "phone", Activity: now}, {ID: "unadmitted", Activity: now}})
	gate.Tick()
	if calls != before {
		t.Fatal("native confirmation performed extra admission IPC")
	}
	if _, ok := gate.leases["unadmitted"]; ok {
		t.Fatal("native handshake allocated registered peer admission")
	}
	gate.leases["unadmitted"] = awgLease{expires: now.Add(-time.Second), activity: now.Add(-time.Minute)}
	oldActivity := gate.leases["unadmitted"].activity
	gate.ObserveActivity([]PeerStatus{{ID: "unadmitted", Activity: now}})
	if !gate.leases["unadmitted"].activity.Equal(oldActivity) {
		t.Fatal("expired lease revived by native activity")
	}
	delete(gate.leases, "unadmitted")
	now = now.Add(2 * time.Second)
	gate.Tick()
	if calls != before+1 {
		t.Fatal("kept-alive peer was expired from original data timestamp")
	}
}
