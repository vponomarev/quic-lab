package admin

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func enrollmentStore(t *testing.T) (*Store, User) {
	t.Helper()
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.Create("enrollment")
	if e != nil {
		t.Fatal(e)
	}
	return s, u
}
func TestConcurrentLastEnrollmentSlot(t *testing.T) {
	s, u := enrollmentStore(t)
	_, token, e := s.NewEnrollment(u.ID, time.Hour, 1)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for _, id := range []string{"import-a", "import-b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if _, e := s.Enroll(token, id, "phone"); e == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("last slot successes=%d", successes)
	}
}
func TestEnrollmentRetry(t *testing.T) {
	s, u := enrollmentStore(t)
	en, token, e := s.NewEnrollment(u.ID, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	if en.MaxDevices != 5 || time.Until(en.Expires) < 24*time.Hour-time.Minute || len(token) != 64 {
		t.Fatalf("defaults/token: %+v len=%d", en, len(token))
	}
	d, e := s.Enroll(token, "stable-import", "phone")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(s.path)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), token) {
		t.Fatal("plaintext enrollment secret persisted")
	}
	reopened, e := OpenStore(strings.TrimSuffix(s.path, "identities.json"))
	if e != nil {
		t.Fatal(e)
	}
	again, e := reopened.Enroll(token, "stable-import", "renamed retry")
	if e != nil {
		t.Fatal(e)
	}
	if d.ID != again.ID || d.Certificate != again.Certificate || d.Key != again.Key {
		t.Fatal("retry changed credentials")
	}
	if _, e := reopened.Enroll(strings.Repeat("0", 64), "stable-import", "phone"); e == nil {
		t.Fatal("request ID replay accepted without token")
	}
	if e := reopened.RevokeEnrollment(en.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := reopened.Enroll(token, "stable-import", "phone"); e == nil {
		t.Fatal("revoked token accepted")
	}
	if got := reopened.Devices(u.ID); len(got) != 2 || got[1].Disabled {
		t.Fatalf("QR revoke affected enrolled devices: %+v", got)
	}
}
func TestEnrollmentSaveRollbackAndPublishedRetry(t *testing.T) {
	s, u := enrollmentStore(t)
	en, token, e := s.NewEnrollment(u.ID, 0, 1)
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	s.syncDir = func(string) error {
		calls++
		if calls == 1 {
			return errors.New("backup sync failed")
		}
		return nil
	}
	if _, e = s.Enroll(token, "retry", "phone"); e == nil {
		t.Fatal("save failure hidden")
	}
	if s.state.Enrollments[en.ID].Used != 0 || len(s.Devices(u.ID)) != 1 {
		t.Fatal("failed save consumed enrollment slot")
	}
	s.syncDir = nil
	d, e := s.Enroll(token, "retry", "phone")
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Enroll(token, "retry", "phone")
	if e != nil || again.ID != d.ID {
		t.Fatal("rollback retry failed", e)
	}
	second, token2, e := s.NewEnrollment(u.ID, 0, 1)
	if e != nil {
		t.Fatal(e)
	}
	calls = 0
	s.syncDir = func(string) error {
		calls++
		if calls%2 == 0 {
			return errors.New("published sync failed")
		}
		return nil
	}
	if _, e = s.Enroll(token2, "published", "phone"); !statePublished(e) {
		t.Fatal("published error lost", e)
	}
	publishedID := s.state.Enrollments[second.ID].Requests["published"]
	published := s.state.Devices[publishedID]
	for attempt := 0; attempt < 3; attempt++ {
		if response, err := s.Enroll(token2, "published", "phone"); err == nil || response.ID != "" {
			t.Fatal("replay acknowledged enrollment before failed durability recovered")
		}
		if s.state.Enrollments[second.ID].Used != 1 || s.state.Enrollments[second.ID].Requests["published"] != publishedID {
			t.Fatal("failed durability replay changed count/identity")
		}
	}
	s.syncDir = nil
	restored, e := s.Enroll(token2, "published", "phone")
	if e != nil || s.state.Enrollments[second.ID].Used != 1 || restored.ID != published.ID || restored.Certificate != published.Certificate || restored.Key != published.Key {
		t.Fatal("recovered durability retry changed credentials", e)
	}
	reopened, e := OpenStore(strings.TrimSuffix(s.path, "identities.json"))
	if e != nil {
		t.Fatal(e)
	}
	durable, e := reopened.Enroll(token2, "published", "phone")
	if e != nil || durable.ID != published.ID || durable.Certificate != published.Certificate || durable.Key != published.Key || reopened.state.Enrollments[second.ID].Used != 1 {
		t.Fatal("reopened enrollment not durable/idempotent", e)
	}
}

func TestEnrollmentAdminControls(t *testing.T) {
	s, u := enrollmentStore(t)
	cfg := config(t)
	web := NewWeb(cfg, s)
	web.sessions["test-session"] = session{CSRF: "csrf", Until: time.Now().Add(time.Hour)}
	cookie := &http.Cookie{Name: "quiclab_admin", Value: "test-session"}
	form := url.Values{"csrf": {"csrf"}, "id": {u.ID}, "ttl_hours": {"6"}, "max_devices": {"2"}}
	qr := call(web.Handler(), "POST", "/users/qr", form.Encode(), cookie)
	if qr.Code != 200 {
		t.Fatal(qr.Body.String())
	}
	enrollments := s.Enrollments(u.ID)
	if len(enrollments) != 1 || enrollments[0].MaxDevices != 2 || time.Until(enrollments[0].Expires) > 6*time.Hour {
		t.Fatal("admin limits not applied", enrollments)
	}
	en := enrollments[0]
	users := call(web.Handler(), "GET", "/users", "", cookie)
	if !strings.Contains(users.Body.String(), en.ID) || !strings.Contains(users.Body.String(), "enrollments/revoke") {
		t.Fatal("admin cannot revisit and revoke existing QR")
	}
	bad := call(web.Handler(), "POST", "/enrollments/revoke", url.Values{"id": {en.ID}}.Encode(), cookie)
	if bad.Code != 403 {
		t.Fatal("QR revoke missing CSRF")
	}
	revoked := call(web.Handler(), "POST", "/enrollments/revoke", url.Values{"id": {en.ID}, "csrf": {"csrf"}}.Encode(), cookie)
	if revoked.Code != 303 || !s.Enrollments(u.ID)[0].Revoked {
		t.Fatal("QR revoke not persisted")
	}
}

func TestRedisplayEnrollmentDoesNotRegisterOrExtend(t *testing.T) {
	s, u := enrollmentStore(t)
	en, token, err := s.NewEnrollment(u.ID, time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWeb(config(t), s)
	w.sessions["test-session"] = session{CSRF: "csrf", Until: time.Now().Add(time.Hour)}
	cookie := &http.Cookie{Name: "quiclab_admin", Value: "test-session"}
	form := url.Values{"csrf": {"csrf"}, "id": {en.ID}}.Encode()
	for i := 0; i < 2; i++ {
		r := call(w.Handler(), "POST", "/enrollments/show", form, cookie)
		if r.Code != 200 || !strings.Contains(r.Body.String(), "enroll#"+token) {
			t.Fatalf("redisplay failed: %d", r.Code)
		}
		if r.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("secret response cacheable")
		}
	}
	after := s.Enrollments(u.ID)[0]
	if after.Used != en.Used || !after.Expires.Equal(en.Expires) || len(s.Enrollments(u.ID)) != 1 {
		t.Fatal("redisplay mutated enrollment")
	}
	if r := call(w.Handler(), "POST", "/enrollments/show", url.Values{"id": {en.ID}}.Encode(), cookie); r.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	if r := call(w.Handler(), "POST", "/enrollments/show", form, nil); r.Code == 200 {
		t.Fatal("anonymous disclosure")
	}
	if err = s.RevokeEnrollment(en.ID); err != nil {
		t.Fatal(err)
	}
	if r := call(w.Handler(), "POST", "/enrollments/show", form, cookie); r.Code != 410 {
		t.Fatal("revoked invitation shown")
	}
}

func TestEnrollmentDisplayPersistenceAndLegacy(t *testing.T) {
	s, u := enrollmentStore(t)
	en, token, err := s.NewEnrollment(u.ID, time.Hour, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.path)
	if strings.Contains(string(raw), token) {
		t.Fatal("plaintext token persisted")
	}
	reopened, err := OpenStore(s.admissionDirectory())
	if err != nil {
		t.Fatal(err)
	}
	metadata, got, err := reopened.EnrollmentInvitation(en.ID)
	if err != nil || got != token || metadata.TokenCipher != "" || metadata.TokenHash != "" {
		t.Fatal("encrypted redisplay failed")
	}
	if _, err = reopened.Enroll(token, "once", "test"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = reopened.EnrollmentInvitation(en.ID); err == nil {
		t.Fatal("exhausted invitation shown")
	}
	en2, _, err := s.NewEnrollment(u.ID, time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	legacy := s.state.Enrollments[en2.ID]
	legacy.TokenCipher = ""
	s.state.Enrollments[en2.ID] = legacy
	s.mu.Unlock()
	if _, _, err = s.EnrollmentInvitation(en2.ID); err == nil {
		t.Fatal("legacy invitation regenerated")
	}
}
