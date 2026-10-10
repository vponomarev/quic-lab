package mdm

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)

func fixture(t *testing.T) (*Store, Binding, string) {
	t.Helper()
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	inv, e := s.CreateInvitation(testNow, Rights{Config: true, VPN: true})
	if e != nil {
		t.Fatal(e)
	}
	secret := strings.Repeat("a", 64)
	b, e := s.Redeem(EnrollmentRequest{Version: 1, Token: inv.Token, RegistrationID: "reg-1", Secret: secret}, testNow)
	if e != nil {
		t.Fatal(e)
	}
	return s, b, secret
}
func TestEnrollmentRetry(t *testing.T) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	inv, _ := s.CreateInvitation(testNow, Rights{VPN: true})
	req := EnrollmentRequest{Version: 1, Token: inv.Token, RegistrationID: "reg", Secret: strings.Repeat("b", 64)}
	b, e := s.Redeem(req, testNow)
	if e != nil {
		t.Fatal(e)
	}
	retry, e := s.Redeem(req, testNow.Add(time.Hour))
	if e != nil || retry.ID != b.ID {
		t.Fatal("retry not idempotent", e)
	}
	req.Secret = strings.Repeat("c", 64)
	if _, e = s.Redeem(req, testNow); e == nil {
		t.Fatal("different secret reused invitation")
	}
	fresh, _ := s.CreateInvitation(testNow, Rights{})
	req.Token = fresh.Token
	if _, e = s.Redeem(req, testNow.Add(25*time.Hour)); e == nil {
		t.Fatal("expired invitation accepted")
	}
	raw, e := os.ReadFile(filepath.Join(s.dir, "state.json"))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, []byte(inv.Token)) || bytes.Contains(raw, []byte(strings.Repeat("b", 64))) {
		t.Fatal("persisted raw credential")
	}
}
func TestResumeDropsCommands(t *testing.T) {
	s, b, _ := fixture(t)
	b, _ = s.Activate(b.ID, testNow)
	c, e := s.QueueVPN(b.ID, "vpn_start", testNow)
	if e != nil {
		t.Fatal(e)
	}
	if c.ExpiresAt.Sub(c.IssuedAt) != 5*time.Minute {
		t.Fatal("wrong TTL")
	}
	if _, e = s.QueueVPN(b.ID, "shell", testNow); e == nil {
		t.Fatal("arbitrary command")
	}
	old := b.Epoch
	b, e = s.Activate(b.ID, testNow.Add(time.Second))
	if e != nil || b.Epoch <= old {
		t.Fatal(e)
	}
	r, e := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch}, testNow.Add(time.Second))
	if e != nil || len(r.Commands) != 0 {
		t.Fatal("old commands replayed", e)
	}
	if _, e = s.Sync(b.ID, SyncRequest{Version: 1, Epoch: old}, testNow); e == nil {
		t.Fatal("stale epoch")
	}
	for i := 0; i < 100; i++ {
		if _, e = s.QueueVPN(b.ID, "vpn_stop", testNow); e != nil {
			t.Fatal(i, e)
		}
	}
	if _, e = s.QueueVPN(b.ID, "vpn_stop", testNow); e == nil {
		t.Fatal("unbounded queue")
	}
}
func TestRevisionCASAndRestart(t *testing.T) {
	s, b, _ := fixture(t)
	r, e := s.SetDesired(b.ID, 0, "current", json.RawMessage(validDocument))
	if e != nil || r.Revision != 1 {
		t.Fatal(e)
	}
	if _, e = s.SetDesired(b.ID, 0, "external", json.RawMessage(validDocument)); e == nil {
		t.Fatal("stale revision accepted")
	}
	if _, e = s.SetDesired(b.ID, 1, "shell", json.RawMessage(validDocument)); e == nil {
		t.Fatal("invalid mode")
	}
	b, _ = s.Activate(b.ID, testNow)
	event := Event{ID: "event-1", Actor: "user", Kind: "vpn_stop", OccurredAt: testNow}
	req := SyncRequest{Version: 1, Epoch: b.Epoch, Events: []Event{event}}
	if _, e = s.Sync(b.ID, req, testNow); e != nil {
		t.Fatal(e)
	}
	reopened, e := OpenStore(s.dir)
	if e != nil {
		t.Fatal(e)
	}
	out, e := reopened.Sync(b.ID, req, testNow)
	if e != nil || out.DesiredConfig == nil || out.DesiredConfig.Revision != 1 {
		t.Fatal("restart lost revision", e)
	}
	audit, e := reopened.Audit(b.ID)
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for _, v := range audit {
		if v.ID == "event-1" {
			n++
		}
	}
	if n != 1 {
		t.Fatal("event replay", n)
	}
	// SetDesired records server wall time, while Sync uses testNow. Expire both.
	pruneAt := time.Now().Add(91 * 24 * time.Hour)
	if bound := testNow.Add(91 * 24 * time.Hour); bound.After(pruneAt) {
		pruneAt = bound
	}
	if e = reopened.Prune(pruneAt); e != nil {
		t.Fatal(e)
	}
	audit, _ = reopened.Audit(b.ID)
	if len(audit) != 0 {
		t.Fatal("old audit retained")
	}
	out, e = reopened.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch}, testNow.Add(91*24*time.Hour))
	if e != nil || out.DesiredConfig == nil {
		t.Fatal("prune deleted binding/config", e)
	}
}
func TestCorruptStoreFailsClosed(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "state.json"), []byte("{bad"), 0600)
	if _, e := OpenStore(dir); e == nil {
		t.Fatal("corrupt store reset")
	}
}
func TestMDMHTTPIsolation(t *testing.T) {
	s, b, secret := fixture(t)
	h := NewHandler(s)
	request := func(path, body, auth string, version uint16) int {
		r := httptest.NewRequest(http.MethodPost, "https://mdm.test/mdm/v1/"+path, strings.NewReader(body))
		r.TLS = &tls.ConnectionState{Version: version}
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	activate := fmt.Sprintf(`{"version":1,"bindingId":%q}`, b.ID)
	if got := request("activate", activate, secret, tls.VersionTLS12); got != 200 {
		t.Fatal(got)
	}
	if got := request("activate", `{"version":1,"bindingId":"other"}`, secret, tls.VersionTLS13); got != 401 {
		t.Fatal("cross-binding", got)
	}
	if got := request("activate", activate, secret, tls.VersionTLS10); got != 426 {
		t.Fatal("weak TLS", got)
	}
	if got := request("activate", strings.Repeat("x", (1<<20)+1), secret, tls.VersionTLS13); got != 413 {
		t.Fatal("body limit", got)
	}
	if got := request("queue", activate, secret, tls.VersionTLS13); got != 404 {
		t.Fatal("device exposed admin", got)
	}
}

// Poll cancellation and a command arriving during a wait must both release workers.
func TestLongPollWakeAndCancel(t *testing.T) {
	s, b, _ := fixture(t)
	b, _ = s.Activate(b.ID, testNow)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, e := s.WaitSync(ctx, b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, Wait: true}, func() time.Time { return testNow })
		done <- e
	}()
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancellation lost")
		}
	case <-time.After(time.Second):
		t.Fatal("poll leak")
	}
	out := make(chan SyncResponse, 1)
	go func() {
		r, e := s.WaitSync(context.Background(), b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, Wait: true}, func() time.Time { return testNow })
		if e == nil {
			out <- r
		}
	}()
	_, e := s.QueueVPN(b.ID, "vpn_start", testNow)
	if e != nil {
		t.Fatal(e)
	}
	select {
	case r := <-out:
		if len(r.Commands) != 1 {
			t.Fatal("missing command")
		}
	case <-time.After(time.Second):
		t.Fatal("lost wakeup")
	}
}
func TestWriteFailureDoesNotPublishRevision(t *testing.T) {
	s, b, _ := fixture(t)
	original := s.dir
	s.dir = filepath.Join(s.dir, "missing", "child")
	if _, e := s.SetDesired(b.ID, 0, "current", json.RawMessage(validDocument)); e == nil {
		t.Fatal("write failure ignored")
	}
	s.dir = original
	r, e := s.SetDesired(b.ID, 0, "current", json.RawMessage(validDocument))
	if e != nil || r.Revision != 1 {
		t.Fatal("failed write changed state", e)
	}
}
