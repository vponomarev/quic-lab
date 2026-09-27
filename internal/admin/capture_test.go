package admin

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureAccessAndDisabled(t *testing.T) {
	c := config(t)
	s, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	w := NewWeb(c, s)
	h := w.Handler()
	for _, path := range []string{"/capture", "/capture/download?id=unknown"} {
		if out := call(h, "GET", path, "", nil); out.Code != 303 {
			t.Fatal("anonymous access", path, out.Code)
		}
	}
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	ss := w.sessions[cookie.Value]
	page := call(h, "GET", "/capture", "", cookie)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "выключена") {
		t.Fatal("disabled page")
	}
	for _, path := range []string{"/capture/start", "/capture/launch", "/capture/stop"} {
		if out := call(h, "POST", path, "id=x", cookie); out.Code != 403 {
			t.Fatal("CSRF bypass", path, out.Code)
		}
	}
	out := call(h, "POST", "/capture/start", url.Values{"csrf": {ss.CSRF}, "kind": {"echo"}}.Encode(), cookie)
	if out.Code != http.StatusServiceUnavailable {
		t.Fatal(out.Code)
	}
	if out = call(h, "POST", "/capture/redeem", "", nil); out.Code != 404 {
		t.Fatal(out.Code)
	}
}
func TestClientSecretEndpointsRetired(t *testing.T) {
	c := config(t)
	store, _ := OpenStore(c.DataDir)
	h := NewWeb(c, store).Handler()
	for _, path := range []string{"/capture/keys", "/capture/enroll", "/capture/qr"} {
		if out := call(h, "POST", path, "", nil); out.Code != 410 {
			t.Fatalf("%s: %d", path, out.Code)
		}
	}
}
func TestHelperDownloadFixedNames(t *testing.T) {
	c := config(t)
	s, _ := OpenStore(c.DataDir)
	w := NewWeb(c, s)
	dir := filepath.Join(c.DataDir, "downloads", "capture")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "quic-lab-capture-windows.zip"), []byte("synthetic zip"), 0600)
	out := call(w.Handler(), "GET", "/download/capture/windows", "", nil)
	if out.Code != 200 || out.Body.String() != "synthetic zip" {
		t.Fatal("download")
	}
	if out = call(w.Handler(), "GET", "/download/capture/secrets", "", nil); out.Code != 404 {
		t.Fatal("arbitrary download")
	}
}
