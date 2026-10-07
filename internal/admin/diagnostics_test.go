package admin

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func diagnosticRequest(w *Web, id, token, body string) *httptest.ResponseRecorder {
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	z.Write([]byte(body))
	z.Close()
	r := httptest.NewRequest("POST", "/api/v1/devices/"+id+"/diagnostics", &b)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Encoding", "gzip")
	rw := httptest.NewRecorder()
	w.Handler().ServeHTTP(rw, r)
	return rw
}
func TestDiagnosticsUploadAuthorizationRetry(t *testing.T) {
	s, w, d, token := updateFixture(t)
	w.Config.DataDir = t.TempDir()
	body := `{"version":1,"records":[{"id":"a","time":1791320000000,"kind":"event","text":"connected"}]}`
	if r := diagnosticRequest(w, d.ID, "", body); r.Code != 401 {
		t.Fatalf("no auth: %d", r.Code)
	}
	for i := 0; i < 2; i++ {
		if r := diagnosticRequest(w, d.ID, token, body); r.Code != 200 {
			t.Fatalf("upload: %d %s", r.Code, r.Body.String())
		}
	}
	files, e := filepath.Glob(filepath.Join(w.Config.DataDir, "client-diagnostics", "*", "*.json"))
	if e != nil || len(files) != 1 {
		t.Fatalf("retry duplicates: %v %v", files, e)
	}
	if e = s.DisableDevice(d.ID); e != nil {
		t.Fatal(e)
	}
	if r := diagnosticRequest(w, d.ID, token, body); r.Code != 403 {
		t.Fatalf("revoked: %d", r.Code)
	}
}
func TestDiagnosticsBoundsAndAdmin(t *testing.T) {
	_, w, d, token := updateFixture(t)
	w.Config.DataDir = t.TempDir()
	for _, body := range []string{`{}`, `{"version":1,"records":[{"id":"a","time":1,"kind":"event","text":"` + strings.Repeat("a", 3000) + `"}]}`, strings.Repeat("a", 1048577)} {
		if r := diagnosticRequest(w, d.ID, token, body); r.Code < 400 {
			t.Fatalf("accepted invalid input: %d", r.Code)
		}
	}
	rw := httptest.NewRecorder()
	w.Handler().ServeHTTP(rw, httptest.NewRequest("GET", "/diagnostics/clients", nil))
	if rw.Code != 303 {
		t.Fatalf("admin auth: %d", rw.Code)
	}
	if files, _ := filepath.Glob(filepath.Join(w.Config.DataDir, "client-diagnostics", "*", "*.json")); len(files) != 0 {
		t.Fatal("invalid upload written")
	}
	_ = os.ErrNotExist
}
func TestDiagnosticsRetentionTimelineAndReopen(t *testing.T) {
	_, w, d, token := updateFixture(t)
	w.Config.DataDir = t.TempDir()
	w.Config.DiagnosticsDays = 1
	body := `{"version":1,"records":[{"id":"x","time":1791320000000,"kind":"event","text":"<script>alert(1)</script>"}]}`
	if r := diagnosticRequest(w, d.ID, token, body); r.Code != 200 {
		t.Fatal(r.Code)
	}
	w.recordServerDiagnostic(d.ID, "gateway_open transport=quic")
	reopened := NewWeb(w.Config, w.Store)
	reopened.sessions["ok"] = session{Until: time.Now().Add(time.Hour)}
	r := httptest.NewRequest("GET", "/diagnostics/clients?device="+d.ID, nil)
	r.AddCookie(&http.Cookie{Name: "quiclab_admin", Value: "ok"})
	rw := httptest.NewRecorder()
	reopened.Handler().ServeHTTP(rw, r)
	if rw.Code != 200 || strings.Contains(rw.Body.String(), "<script>alert") || !strings.Contains(rw.Body.String(), "&lt;script&gt;") || !strings.Contains(rw.Body.String(), "gateway_open") {
		t.Fatalf("timeline: %d %s", rw.Code, rw.Body.String())
	}
	files, _ := w.diagnosticFiles()
	if len(files) != 2 {
		t.Fatal(len(files))
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, f := range files {
		os.Chtimes(f.path, old, old)
	}
	diagnosticDiskMu.Lock()
	e := w.pruneDiagnostics(0)
	diagnosticDiskMu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	files, _ = w.diagnosticFiles()
	if len(files) != 0 {
		t.Fatal("retention")
	}
}

func TestDiagnosticsRetryMustResyncDirectory(t *testing.T) {
	_, w, d, token := updateFixture(t)
	w.Config.DataDir = t.TempDir()
	w.diagnosticSync = func(string) error { return fmt.Errorf("simulated directory sync failure") }
	body := `{"version":1,"records":[{"id":"sync","time":1791320000000,"kind":"event","text":"connected"}]}`
	if r := diagnosticRequest(w, d.ID, token, body); r.Code != 503 {
		t.Fatalf("ack before durable directory: %d", r.Code)
	}
	count := 0
	w.diagnosticSync = func(string) error { count++; return nil }
	if r := diagnosticRequest(w, d.ID, token, body); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if count < 3 {
		t.Fatalf("retry skipped parent directory sync: %d", count)
	}
}

func TestDiagnosticsUserNavigation(t *testing.T) {
	s, w, d, _ := updateFixture(t)
	w.Config.DataDir = t.TempDir()
	u := s.state.Users[d.UserID]
	u.Name = "Owner <Alice>"
	s.state.Users[d.UserID] = u
	s.state.Users["other"] = User{ID: "other", Name: "Other owner"}
	s.state.Devices["other-device"] = Device{ID: "other-device", UserID: "other", Name: "Foreign phone"}
	w.sessions["nav"] = session{Until: time.Now().Add(time.Hour)}
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "quiclab_admin", Value: "nav"})
		rw := httptest.NewRecorder()
		w.Handler().ServeHTTP(rw, r)
		return rw
	}
	all := get("/diagnostics/clients")
	if all.Code != 200 || !strings.Contains(all.Body.String(), "Owner &lt;Alice&gt;") || !strings.Contains(all.Body.String(), "Other owner") {
		t.Fatal("missing escaped user labels")
	}
	own := get("/diagnostics/clients?user=" + d.UserID)
	if own.Code != 200 || strings.Contains(own.Body.String(), "Foreign phone") || !strings.Contains(own.Body.String(), d.ID) || !strings.Contains(own.Body.String(), `name="user"`) {
		t.Fatal("user filter/navigation missing")
	}
	if get("/diagnostics/clients?user="+d.UserID+"&device=other-device").Code != 404 {
		t.Fatal("device outside selected user")
	}
	if get("/diagnostics/clients?user=missing").Code != 404 {
		t.Fatal("unknown user")
	}
}

func TestDeviceReportSummaryUsesClientReceipt(t *testing.T) {
	_, w, d, token := updateFixture(t)
	w.Config.DataDir = t.TempDir()
	body := `{"version":1,"records":[{"id":"latest","time":1,"kind":"event","text":"test"}]}`
	if r := diagnosticRequest(w, d.ID, token, body); r.Code != 200 {
		t.Fatal(r.Code)
	}
	fs, _ := w.diagnosticFiles()
	stamp := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(fs[0].path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	w.recordServerDiagnostic(d.ID, "server-only-event")
	rw := httptest.NewRecorder()
	w.page(rw, view{Admin: true, Users: w.Store.List()})
	if !strings.Contains(rw.Body.String(), `data-last-report="2026-10-07T00:00:00Z"`) {
		t.Fatal("missing client receipt timestamp")
	}
	if !strings.Contains(rw.Body.String(), "Регистрация:") {
		t.Fatal("missing registration date")
	}
}

func TestDiagnosticClientVersion(t *testing.T) {
	_, w, d, token := updateFixture(t)
	w.Config.DataDir = t.TempDir()
	body := `{"version":1,"records":[{"id":"versioned","time":1791320000000,"kind":"event","text":"connected","app_version":"0.8.1-pre.3","app_version_code":36}]}`
	if r := diagnosticRequest(w, d.ID, token, body); r.Code != 200 {
		t.Fatalf("versioned upload: %d %s", r.Code, r.Body.String())
	}
	body = strings.ReplaceAll(body, "0.8.1-pre.3", strings.Repeat("x", 129))
	if r := diagnosticRequest(w, d.ID, token, body); r.Code != 400 {
		t.Fatalf("invalid version accepted: %d", r.Code)
	}
}

func TestDiagnosticVersionDoesNotRegressOnDelayedUpload(t *testing.T) {
	s, w, d, token := updateFixture(t)
	w.Config.DataDir = t.TempDir()
	for _, b := range []string{
		`{"version":1,"records":[{"id":"new","time":2000,"kind":"event","text":"new","app_version":"new","app_version_code":36}]}`,
		`{"version":1,"records":[{"id":"old","time":1000,"kind":"event","text":"old","app_version":"old","app_version_code":35}]}`,
		`{"version":1,"records":[{"id":"legacy","time":3000,"kind":"event","text":"legacy"}]}`,
	} {
		if r := diagnosticRequest(w, d.ID, token, b); r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	versions, counts := w.deviceVersions(s.List())
	if versions[d.ID] != "new (36)" || counts["new (36)"] != 1 {
		t.Fatalf("%v %v", versions, counts)
	}
	if diagnosticVersionText(diagnosticRecord{AppVersion: "old", AppVersionCode: 35, Text: "test"}) != "[App old (35)] test" {
		t.Fatal("missing historical version")
	}
}
