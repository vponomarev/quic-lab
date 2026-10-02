package admin

import (
	"context"
	"crypto/tls"
	"net/url"
	"os"
	"path/filepath"
	"quiclab/internal/debugcapture"
	"strings"
	"testing"
)

// captureLifecycle is separate from display transport names: HTTPS and QUIC
// can each carry either a standalone connection or a multiplexed bond.
type captureLifecycle interface {
	CaptureEligible(string) error
	TrackMultiplexed(tls.ConnectionState) (func(), error)
	ConfigureCapture(*debugcapture.Manager)
}

func captureBondFixture(t *testing.T) (*Store, *Web, User, captureLifecycle) {
	t.Helper()
	cfg := config(t)
	store, err := OpenStore(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateWithProtocols("capture bond", []string{"quic", "https"})
	if err != nil {
		t.Fatal(err)
	}
	web := NewWeb(cfg, store)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	exe := filepath.Join(t.TempDir(), "tcpdump")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nprintf '\\324\\303\\262\\241\\002\\000\\004\\000\\000\\000\\000\\000\\000\\000\\000\\000\\377\\377\\000\\000\\001\\000\\000\\000'\nexec sleep 120\n"), 0700); err != nil {
		t.Fatal(err)
	}
	web.Capture, err = debugcapture.New(ctx, debugcapture.Config{Interface: "lo", TCPDump: exe, Slots: 4, Ports: map[string][2]int{"vpn": {12443, 12444}, "echo": {12445, 12446}}}, func(*debugcapture.Session) (func(), error) { return func() {}, nil })
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, ok := any(store).(captureLifecycle)
	if !ok {
		t.Fatal("Store lacks explicit multiplexed capture lifecycle")
	}
	lifecycle.ConfigureCapture(web.Capture)
	return store, web, user, lifecycle
}

func TestCaptureRejectsBond(t *testing.T) {
	store, web, user, lifecycle := captureBondFixture(t)
	device, err := store.CreateDevice(user.ID, "phone")
	if err != nil {
		t.Fatal(err)
	}
	release, err := lifecycle.TrackMultiplexed(deviceTLS(t, device.Certificate))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, target := range []string{device.ID, user.ID, ""} {
		if err := lifecycle.CaptureEligible(target); err == nil || !strings.Contains(strings.ToLower(err.Error()), "multiplexed") {
			t.Fatalf("target %q: unclear bond rejection: %v", target, err)
		}
	}
	h := web.Handler()
	cookie := call(h, "POST", "/login", url.Values{"username": {web.Config.Username}, "password": {web.Config.Password}}.Encode(), nil).Result().Cookies()[0]
	csrf := web.sessions[cookie.Value].CSRF
	for _, path := range []string{"/users/capture", "/capture/start"} {
		out := call(h, "POST", path, url.Values{"csrf": {csrf}, "id": {user.ID}, "action": {"start"}, "kind": {"vpn"}}.Encode(), cookie)
		if out.Code != 409 || !strings.Contains(strings.ToLower(out.Body.String()), "multiplexed") {
			t.Fatalf("%s: %d %s", path, out.Code, out.Body.String())
		}
	}
	if lifecycle.CaptureEligible(device.ID) == nil {
		t.Fatal("capture request disabled bond state")
	}
	release()
	if err := lifecycle.CaptureEligible(device.ID); err != nil {
		t.Fatal("bond release did not restore capture", err)
	}
}

func TestCaptureStopsWhenTargetEntersBond(t *testing.T) {
	store, web, user, lifecycle := captureBondFixture(t)
	other, err := store.CreateWithProtocols("other", []string{"quic"})
	if err != nil {
		t.Fatal(err)
	}
	own, err := web.Capture.StartUser("teacher", user.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := web.Capture.StartUser("other teacher", other.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	global, err := web.Capture.Start("global teacher", "vpn", "")
	if err != nil {
		t.Fatal(err)
	}
	release, err := lifecycle.TrackMultiplexed(deviceTLS(t, user.Certificate))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, session := range []*debugcapture.Session{own, global} {
		if state := session.Snapshot().State; !strings.Contains(strings.ToLower(state), "multiplexed") {
			t.Fatalf("capture not stopped with explicit reason: %s", state)
		}
	}
	if unrelated.Snapshot().State != "running" {
		t.Fatal("unrelated standalone capture stopped")
	}
	if lifecycle.CaptureEligible(user.ID) == nil {
		t.Fatal("transition stopped VPN bond")
	}
}

func TestCaptureStartTransitionRace(t *testing.T) {
	store, web, user, lifecycle := captureBondFixture(t)
	cs := deviceTLS(t, user.Certificate)
	started := make(chan struct{})
	unblock := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- store.WithCaptureEligible(user.ID, func() error {
			close(started)
			<-unblock
			_, err := web.Capture.StartUser("teacher", user.ID, "", "")
			return err
		})
	}()
	<-started
	transition := make(chan func(), 1)
	go func() {
		release, err := lifecycle.TrackMultiplexed(cs)
		if err != nil {
			done <- err
			return
		}
		transition <- release
	}()
	close(unblock)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	release := <-transition
	defer release()
	if web.Capture.UserCapture(user.ID) != nil {
		t.Fatal("racing capture survived bond entry")
	}
	if err := store.WithCaptureEligible(user.ID, func() error { t.Fatal("start callback invoked while bond active"); return nil }); err == nil {
		t.Fatal("racing transition eligibility bypass")
	}
}

func TestCaptureDeviceScopeAndConcurrentBonds(t *testing.T) {
	store, _, user, lifecycle := captureBondFixture(t)
	device, err := store.CreateDevice(user.ID, "phone")
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := store.CreateDevice(user.ID, "tablet")
	if err != nil {
		t.Fatal(err)
	}
	release1, err := lifecycle.TrackMultiplexed(deviceTLS(t, device.Certificate))
	if err != nil {
		t.Fatal(err)
	}
	release2, err := lifecycle.TrackMultiplexed(deviceTLS(t, device.Certificate))
	if err != nil {
		t.Fatal(err)
	}
	defer release1()
	defer release2()
	if lifecycle.CaptureEligible(sibling.ID) == nil {
		t.Fatal("userwide capture bypassed sibling bond")
	}
	release1()
	release1()
	if lifecycle.CaptureEligible(device.ID) == nil {
		t.Fatal("release removed another active logical bond")
	}
	release2()
	if err := lifecycle.CaptureEligible(user.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureSharedListenerCannotBypassBond(t *testing.T) {
	_, web, user, lifecycle := captureBondFixture(t)
	web.Capture.Config.Ports["echo"] = [2]int{12443, 12446}
	shared, err := web.Capture.Start("teacher", "echo", "")
	if err != nil {
		t.Fatal(err)
	}
	release, err := lifecycle.TrackMultiplexed(deviceTLS(t, user.Certificate))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if !strings.Contains(shared.Snapshot().State, "multiplexed") {
		t.Fatal("overlapping Echo capture survived bond")
	}
	h := web.Handler()
	cookie := call(h, "POST", "/login", url.Values{"username": {web.Config.Username}, "password": {web.Config.Password}}.Encode(), nil).Result().Cookies()[0]
	out := call(h, "POST", "/capture/start", url.Values{"csrf": {web.sessions[cookie.Value].CSRF}, "kind": {"echo"}}.Encode(), cookie)
	if out.Code != 409 {
		t.Fatalf("shared listener bypass: %d %s", out.Code, out.Body.String())
	}
}

func TestCaptureSeparateEchoListenerRemainsAvailable(t *testing.T) {
	_, web, user, lifecycle := captureBondFixture(t)
	release, err := lifecycle.TrackMultiplexed(deviceTLS(t, user.Certificate))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	h := web.Handler()
	cookie := call(h, "POST", "/login", url.Values{"username": {web.Config.Username}, "password": {web.Config.Password}}.Encode(), nil).Result().Cookies()[0]
	out := call(h, "POST", "/capture/start", url.Values{"csrf": {web.sessions[cookie.Value].CSRF}, "kind": {"echo"}}.Encode(), cookie)
	if out.Code != 303 {
		t.Fatalf("separate Echo listener incorrectly blocked: %d %s", out.Code, out.Body.String())
	}
}
