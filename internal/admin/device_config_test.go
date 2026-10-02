package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func updateFixture(t *testing.T) (*Store, *Web, Device, string) {
	t.Helper()
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.Create("update-owner")
	if e != nil {
		t.Fatal(e)
	}
	_, enrollment, e := s.NewEnrollment(u.ID, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	d, token, e := s.EnrollWithUpdate(enrollment, "request-update", "phone")
	if e != nil {
		t.Fatal(e)
	}
	w := NewWeb(Config{PublicURL: "https://control.example/admin/", VPN: Profile{Version: 2, Kind: "vpn", Hostname: "vpn.example", QUIC: "vpn.example:443", HTTPS: "vpn.example:443", Transports: []string{"quic", "https"}}}, s)
	return s, w, d, token
}
func updateRequest(w *Web, id, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/api/v1/devices/"+id+"/config", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rw := httptest.NewRecorder()
	w.Handler().ServeHTTP(rw, r)
	return rw
}
func TestUUIDAloneDenied(t *testing.T) {
	_, w, d, _ := updateFixture(t)
	if rw := updateRequest(w, d.ID, ""); rw.Code != http.StatusUnauthorized {
		t.Fatalf("UUID alone: status %d", rw.Code)
	}
	if rw := updateRequest(w, d.ID, strings.Repeat("a", 64)); rw.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status %d", rw.Code)
	}
}
func TestDisabledDeviceUpdateDenied(t *testing.T) {
	s, w, d, token := updateFixture(t)
	if e := s.DisableDevice(d.ID); e != nil {
		t.Fatal(e)
	}
	if rw := updateRequest(w, d.ID, token); rw.Code != http.StatusForbidden {
		t.Fatalf("disabled: status %d", rw.Code)
	}
	if rw := updateRequest(w, d.ID, strings.Repeat("a", 64)); rw.Code != http.StatusUnauthorized {
		t.Fatalf("disabled wrong token: status %d", rw.Code)
	}
}
func TestUpdateEnrollmentReplayAfterReopenHashOnly(t *testing.T) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.Create("owner")
	if e != nil {
		t.Fatal(e)
	}
	_, enrollment, e := s.NewEnrollment(u.ID, 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	d, token, e := s.EnrollWithUpdate(enrollment, "lost-response", "phone")
	if e != nil {
		t.Fatal(e)
	}
	if len(token) != 64 {
		t.Fatal("expected 256-bit hex token")
	}
	data, e := os.ReadFile(s.path)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(data, []byte(token)) || bytes.Contains(data, []byte(enrollment)) {
		t.Fatal("raw bearer persisted")
	}
	reopened, e := OpenStore(filepath.Dir(s.path))
	if e != nil {
		t.Fatal(e)
	}
	replay, replayed, e := reopened.EnrollWithUpdate(enrollment, "lost-response", "ignored")
	if e != nil {
		t.Fatal(e)
	}
	if replay.ID != d.ID || replayed != token {
		t.Fatal("retry changed device/token")
	}
	if _, e = reopened.AuthenticateUpdate(d.ID, token); e != nil {
		t.Fatal(e)
	}
	metadata, _ := json.Marshal(reopened.Devices(u.ID))
	if bytes.Contains(metadata, []byte(token)) {
		t.Fatal("metadata leaked bearer")
	}
}
func TestDeviceConfigEnvelopeAndRevision(t *testing.T) {
	_, w, d, token := updateFixture(t)
	first := updateRequest(w, d.ID, token)
	if first.Code != 200 {
		t.Fatalf("status %d: %s", first.Code, first.Body.String())
	}
	var a map[string]any
	if e := json.Unmarshal(first.Body.Bytes(), &a); e != nil {
		t.Fatal(e)
	}
	if a["schema_version"] != float64(1) || a["device_id"] != d.ID || a["exit_id"] != d.ID || a["server_config"] == nil || a["capabilities"] == nil {
		t.Fatalf("invalid envelope: %v", a)
	}
	if strings.Contains(first.Body.String(), token) {
		t.Fatal("update response leaked bearer")
	}
	revision := a["config_revision"]
	same := updateRequest(w, d.ID, token)
	var b map[string]any
	json.Unmarshal(same.Body.Bytes(), &b)
	if revision != b["config_revision"] {
		t.Fatal("unchanged config changed revision")
	}
	w.Config.VPN.DNS = "9.9.9.9"
	changed := updateRequest(w, d.ID, token)
	json.Unmarshal(changed.Body.Bytes(), &b)
	if b["config_revision"].(float64) <= revision.(float64) {
		t.Fatal("changed config did not advance revision")
	}
}

func TestDeviceConfigPublishedFailureRetrySyncs(t *testing.T) {
	s, w, d, token := updateFixture(t)
	if rw := updateRequest(w, d.ID, token); rw.Code != 200 {
		t.Fatalf("initial status %d", rw.Code)
	}
	w.Config.VPN.DNS = "9.9.9.9"
	calls := 0
	s.syncDir = func(string) error {
		calls++
		if calls == 2 {
			return os.ErrPermission
		}
		return nil
	}
	if rw := updateRequest(w, d.ID, token); rw.Code != http.StatusServiceUnavailable {
		t.Fatalf("published sync failure: status %d", rw.Code)
	}
	before := calls
	s.syncDir = func(string) error { calls++; return nil }
	if rw := updateRequest(w, d.ID, token); rw.Code != 200 {
		t.Fatalf("retry status %d", rw.Code)
	}
	if calls <= before {
		t.Fatal("retry acknowledged unsynced published config")
	}
}
