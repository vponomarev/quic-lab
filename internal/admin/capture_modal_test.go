package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"quiclab/internal/debugcapture"
	"strings"
	"testing"
)

func TestCaptureModalJSONAndSettings(t *testing.T) {
	c := config(t)
	store, _ := OpenStore(c.DataDir)
	u, _ := store.Create("modal")
	w := NewWeb(c, store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exe, _ := os.Executable()
	w.Capture, _ = debugcapture.New(ctx, debugcapture.Config{Interface: "lo", TCPDump: exe, Slots: 2}, nil)
	h := w.Handler()
	if out := call(h, "GET", "/capture/settings", "", nil); out.Code != 303 {
		t.Fatal("anonymous settings")
	}
	cookie := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil).Result().Cookies()[0]
	csrf := w.sessions[cookie.Value].CSRF
	request := func(path string, v url.Values) *httptest.ResponseRecorder {
		method := "GET"
		if v != nil {
			method = "POST"
		}
		r := httptest.NewRequest(method, "https://lab.example"+path, strings.NewReader(v.Encode()))
		r.AddCookie(cookie)
		r.Header.Set("Origin", "https://lab.example")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Accept", "application/json")
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		return out
	}
	out := request("/users/capture", url.Values{"csrf": {csrf}, "id": {u.ID}, "action": {"start"}})
	var launch struct{ ID, Link string }
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &launch) != nil || launch.Link == "" {
		t.Fatal("JSON start", out.Code, out.Body.String())
	}
	out = request("/capture/state", nil)
	if out.Code != 200 || strings.Contains(out.Body.String(), csrf) || !strings.Contains(out.Body.String(), launch.ID) {
		t.Fatal("state privacy", out.Code)
	}
	out = request("/capture/stop", url.Values{"csrf": {csrf}, "id": {launch.ID}})
	if out.Code != http.StatusOK || w.Capture.UserCapture(u.ID) != nil {
		t.Fatal("stop without redirect")
	}
	out = call(h, "GET", "/capture/settings", "", cookie)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "settings-helper") {
		t.Fatal("settings page")
	}
}
