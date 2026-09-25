package admin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicAPKDownloadAndHiddenQR(t *testing.T) {
	cfg := config(t)
	cfg.APKPath = filepath.Join(t.TempDir(), "quic-lab.apk")
	store, e := OpenStore(cfg.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	web := NewWeb(cfg, store)
	h := web.Handler()
	if w := call(h, "GET", "/download/quic-lab.apk", "", nil); w.Code != 404 {
		t.Fatal("missing APK", w.Code)
	}
	if w := call(h, "GET", "/", "", nil); strings.Contains(w.Body.String(), "Показать QR для скачивания") {
		t.Fatal("missing APK advertised")
	}
	payload := []byte("APK test content")
	if e = os.WriteFile(cfg.APKPath, payload, 0600); e != nil {
		t.Fatal(e)
	}
	page := call(h, "GET", "/", "", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), `<details class="download-qr">`) || strings.Contains(page.Body.String(), `<details class="download-qr" open`) || strings.Count(page.Body.String(), "data:image/png;base64,") != 2 || !strings.Contains(page.Body.String(), `href="/admin/download/quic-lab.apk"`) {
		t.Fatal("download UI", page.Body.String())
	}
	w := call(h, "GET", "/download/quic-lab.apk", "", nil)
	if w.Code != 200 || w.Body.String() != string(payload) || w.Header().Get("Content-Type") != "application/vnd.android.package-archive" || !strings.Contains(w.Header().Get("Content-Disposition"), "quic-lab.apk") {
		t.Fatal("public download", w.Code)
	}
	r := httptest.NewRequest("GET", "/download/quic-lab.apk", nil)
	r.Header.Set("Range", "bytes=0-2")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, r)
	if rw.Code != http.StatusPartialContent || rw.Body.String() != "APK" {
		t.Fatal("range support", rw.Code)
	}
	head := call(h, "HEAD", "/download/quic-lab.apk", "", nil)
	if head.Code != 200 || head.Body.Len() != 0 {
		t.Fatal("HEAD", head.Code)
	}
	if w := call(h, "GET", "/download/identities.json", "", nil); w.Code != 404 {
		t.Fatal("unexpected public file")
	}
	// Replacement is opened on each request, so no restart is needed.
	if e = os.WriteFile(cfg.APKPath, []byte("New APK"), 0600); e != nil {
		t.Fatal(e)
	}
	if w := call(h, "GET", "/download/quic-lab.apk", "", nil); w.Body.String() != "New APK" {
		t.Fatal("old APK cached")
	}
}
