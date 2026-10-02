package admin

import (
	"crypto/tls"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDeviceRegisterRevokeRace(t *testing.T) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.CreateWithProtocols("owner", []string{"quic"})
	if e != nil {
		t.Fatal(e)
	}
	cs := deviceTLS(t, u.Certificate)
	var successes, closed atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, e := s.RegisterProtocol(cs, "quic", func() { closed.Add(1) }); e == nil {
				successes.Add(1)
			}
		}()
	}
	close(start)
	if e = s.DisableDevice(u.ID); e != nil {
		t.Fatal(e)
	}
	wg.Wait()
	if successes.Load() != closed.Load() {
		t.Fatalf("active registrations escaped revocation: admitted=%d closed=%d", successes.Load(), closed.Load())
	}
	if _, e = s.RegisterProtocol(cs, "quic", func() {}); e == nil {
		t.Fatal("join active after revoke")
	}
}
func TestDeviceReloadFailureRemainsRevoked(t *testing.T) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s.ConfigureAWG(awgConfig())
	u, e := s.CreateWithProtocols("owner", []string{"awg", "quic"})
	if e != nil {
		t.Fatal(e)
	}
	s.SetAWGReload(func() error { return errors.New("worker unavailable") })
	if e = s.DisableDevice(u.ID); e == nil {
		t.Fatal("reload failure hidden")
	}
	if s.Verify(deviceTLS(t, u.Certificate)) == nil {
		t.Fatal("reload failure restored revoked identity")
	}
	if _, e = s.Register(tls.ConnectionState{}, func() {}); e == nil {
		t.Fatal("unverified identity allowed")
	}
}
func TestDevicePolicyProvisionsEveryPeer(t *testing.T) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s.ConfigureAWG(awgConfig())
	u, e := s.CreateWithProtocols("owner", []string{"quic"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.CreateDevice(u.ID, "second")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetProtocols(u.ID, []string{"quic", "awg"}, false); e != nil {
		t.Fatal(e)
	}
	used := map[string]bool{}
	for _, d := range s.Devices(u.ID) {
		if d.AWG == nil {
			t.Fatal("protocol enable skipped sibling AWG provisioning")
		}
		if used[d.AWG.Address] {
			t.Fatal("duplicate peer address")
		}
		used[d.AWG.Address] = true
	}
}
