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
