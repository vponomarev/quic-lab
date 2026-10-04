package admin

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"quiclab/internal/vlessserver"
	"testing"
	"time"
)

type failingVLESSRoutes struct {
	fail  bool
	calls int
}

func (r *failingVLESSRoutes) Validate(vlessserver.Config) error { return nil }
func (r *failingVLESSRoutes) Apply(_ context.Context, _ vlessserver.Config, _ uint64) error {
	r.calls++
	if r.fail {
		r.fail = false
		return errors.New("route rejected")
	}
	return nil
}
func TestVLESSApplyRollbackAndStaleRevision(t *testing.T) {
	s, _ := managedVLESSFixture(t)
	routes := &failingVLESSRoutes{fail: true}
	s.SetVLESSRouteController(routes)
	old := s.vlessConfig()
	next := old
	next.RealityShortIDs = []string{"ccdd"}
	_, _, rev := s.VLESSStatus()
	if err := s.ApplyVLESSConfig(context.Background(), next, rev); err == nil {
		t.Fatal("failed routes reported success")
	}
	if s.vlessConfig().RealityShortIDs[0] != "aabb" || s.state.VLESSPending != nil {
		t.Fatal("rollback incomplete")
	}
	if routes.calls < 2 {
		t.Fatal("rollback not applied")
	}
	if err := s.ApplyVLESSConfig(context.Background(), next, rev); err == nil {
		t.Fatal("stale form accepted")
	}
	_, _, rev = s.VLESSStatus()
	if err := s.ApplyVLESSConfig(context.Background(), next, rev); err != nil {
		t.Fatal(err)
	}
	if s.vlessConfig().RealityShortIDs[0] != "ccdd" {
		t.Fatal("successful settings lost")
	}
}

type blockingVLESSRoutes struct {
	entered chan struct{}
	release chan struct{}
	first   bool
}

func (r *blockingVLESSRoutes) Validate(vlessserver.Config) error { return nil }
func (r *blockingVLESSRoutes) Apply(_ context.Context, _ vlessserver.Config, _ uint64) error {
	if !r.first {
		r.first = true
		close(r.entered)
		<-r.release
		return errors.New("injected route failure")
	}
	return nil
}
func TestVLESSRollbackPreservesConcurrentRevocation(t *testing.T) {
	s, _ := managedVLESSFixture(t)
	u, err := s.CreateWithProtocols("fixture", []string{"vless"})
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := s.NewEnrollment(u.ID, time.Hour, 5)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Enroll(token, "phone", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	routes := &blockingVLESSRoutes{entered: make(chan struct{}), release: make(chan struct{})}
	s.SetVLESSRouteController(routes)
	next := s.vlessConfig()
	next.RealityShortIDs = []string{"ccdd"}
	_, _, rev := s.VLESSStatus()
	done := make(chan error, 1)
	go func() { done <- s.ApplyVLESSConfig(context.Background(), next, rev) }()
	select {
	case <-routes.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("apply did not reach route boundary")
	}
	revoked := make(chan error, 1)
	go func() { revoked <- s.DisableDevice(d.ID) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.mu.Lock()
		disabled := s.state.Devices[d.ID].Disabled
		s.mu.Unlock()
		if disabled {
			break
		}
		if time.Now().After(deadline) {
			close(routes.release)
			t.Fatal("revocation blocked on config transaction")
		}
		time.Sleep(time.Millisecond)
	}
	close(routes.release)
	if err := <-done; err == nil {
		t.Fatal("failure reported success")
	}
	if err := <-revoked; err != nil {
		t.Fatal(err)
	}
	if _, err := s.VLESSProfile(d.ID); err == nil {
		t.Fatal("rollback restored revoked device")
	}
}
func TestVLESSApplyRecoveryAfterRestart(t *testing.T) {
	for _, applied := range []bool{false, true} {
		t.Run(fmt.Sprint(applied), func(t *testing.T) {
			s, _ := managedVLESSFixture(t)
			previous := s.vlessConfig()
			next := cloneVLESSConfig(previous)
			next.RealityShortIDs = []string{"ccdd"}
			s.mu.Lock()
			s.state.VLESSPending = &vlessPendingConfig{Previous: previous}
			s.state.VLESS = &next
			err := s.save()
			s.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if applied {
				if err = s.SyncVLESS(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := OpenStore(filepath.Dir(s.path))
			if err != nil {
				t.Fatal(err)
			}
			reopened.SetVLESSRouteController(&failingVLESSRoutes{})
			if err = reopened.ConfigureVLESS(&previous, s.vlessClient); err != nil {
				t.Fatal(err)
			}
			if reopened.state.VLESSPending != nil || reopened.vlessConfig().RealityShortIDs[0] != "aabb" {
				t.Fatal("restart failed to restore previous config")
			}
			_, ok, _ := reopened.VLESSStatus()
			if !ok {
				t.Fatal("recovery not confirmed")
			}
		})
	}
}

func TestVLESSApplyPublishedPrepareFailureRecovers(t *testing.T) {
	s, _ := managedVLESSFixture(t)
	old := s.vlessConfig()
	next := cloneVLESSConfig(old)
	next.RealityShortIDs = []string{"ccdd"}
	calls := 0
	s.syncDir = func(string) error {
		calls++
		if calls == 2 {
			return errors.New("injected fsync failure")
		}
		return nil
	}
	_, _, rev := s.VLESSStatus()
	if err := s.ApplyVLESSConfig(context.Background(), next, rev); err == nil {
		t.Fatal("durability failure hidden")
	}
	if s.state.VLESSPending != nil || s.vlessConfig().RealityShortIDs[0] != "aabb" {
		t.Fatal("published prepare failure requires unnecessary restart")
	}
}

type unavailableVLESSRoutes struct{}

func (*unavailableVLESSRoutes) Validate(vlessserver.Config) error { return nil }
func (*unavailableVLESSRoutes) Apply(context.Context, vlessserver.Config, uint64) error {
	return errors.New("unavailable")
}
func TestVLESSExportsRequireCommittedRevision(t *testing.T) {
	s, _ := managedVLESSFixture(t)
	u, err := s.CreateWithProtocols("export-fixture", []string{"vless"})
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := s.NewEnrollment(u.ID, time.Hour, 5)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Enroll(token, "phone", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.VLESSProfile(d.ID); err != nil {
		t.Fatal(err)
	}
	s.SetVLESSRouteController(&unavailableVLESSRoutes{})
	next := cloneVLESSConfig(s.vlessConfig())
	next.RealityShortIDs = []string{"ccdd"}
	_, _, rev := s.VLESSStatus()
	if err = s.ApplyVLESSConfig(context.Background(), next, rev); err == nil {
		t.Fatal("recovery failure hidden")
	}
	if s.state.VLESSPending == nil {
		t.Fatal("failed recovery lost durable intent")
	}
	if _, ok, _ := s.VLESSStatus(); ok {
		t.Fatal("failed recovery shown as applied")
	}
	if _, err = s.VLESSProfile(d.ID); err == nil {
		t.Fatal("unconfirmed config exported")
	}
	s.SetVLESSRouteController(&failingVLESSRoutes{})
	if err = s.recoverVLESSConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.VLESSProfile(d.ID); err != nil {
		t.Fatal(err)
	}
}
