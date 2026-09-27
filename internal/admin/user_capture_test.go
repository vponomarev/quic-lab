package admin

import (
	"context"
	"net/url"
	"os"
	"quiclab/internal/debugcapture"
	"strings"
	"testing"
)

func TestUserCaptureAccessLifecycle(t *testing.T) {
	c := config(t)
	store, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	u, e := store.CreateWithProtocols("capture user", []string{"quic", "https"})
	if e != nil {
		t.Fatal(e)
	}
	w := NewWeb(c, store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exe, _ := os.Executable()
	w.Capture, e = debugcapture.New(ctx, debugcapture.Config{Interface: "lo", TCPDump: exe, Slots: 2}, nil)
	if e != nil {
		t.Fatal(e)
	}
	h := w.Handler()
	first := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil).Result().Cookies()[0]
	second := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil).Result().Cookies()[0]
	csrf := w.sessions[first.Value].CSRF
	other := w.sessions[second.Value].CSRF
	vals := url.Values{"csrf": {csrf}, "id": {u.ID}, "action": {"start"}}
	if out := call(h, "GET", "/users/captures", "", nil); out.Code != 303 {
		t.Fatal("anonymous stats")
	}
	if out := call(h, "POST", "/users/capture", "id="+u.ID, first); out.Code != 403 {
		t.Fatal("CSRF bypass")
	}
	out := call(h, "POST", "/users/capture", vals.Encode(), first)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "quic-lab://capture") {
		t.Fatal("start", out.Code, out.Body.String())
	}
	s := w.Capture.UserCapture(u.ID)
	if s == nil {
		t.Fatal("missing capture")
	}
	for _, action := range []string{"link", "stop", "start"} {
		vals.Set("action", action)
		vals.Set("csrf", other)
		if out := call(h, "POST", "/users/capture", vals.Encode(), second); out.Code != 403 {
			t.Fatal("cross teacher", action, out.Code)
		}
	}
	if out := call(h, "GET", "/capture/download?id="+s.Snapshot().ID, "", second); out.Code != 404 {
		t.Fatal("cross download")
	}
	out = call(h, "GET", "/users/captures", "", second)
	if !strings.Contains(out.Body.String(), `"owned":false`) {
		t.Fatal("missing other owner indication")
	}
	vals.Set("csrf", csrf)
	vals.Set("action", "stop")
	if out := call(h, "POST", "/users/capture", vals.Encode(), first); out.Code != 303 {
		t.Fatal("stop", out.Code)
	}
	if w.Capture.UserCapture(u.ID) != nil {
		t.Fatal("still active")
	}
	vals.Set("action", "start")
	call(h, "POST", "/users/capture", vals.Encode(), first)
	call(h, "POST", "/users/toggle", url.Values{"csrf": {csrf}, "id": {u.ID}, "disabled": {"true"}}.Encode(), first)
	if w.Capture.UserCapture(u.ID) != nil {
		t.Fatal("disable did not stop capture")
	}
	if out := call(h, "POST", "/users/capture", vals.Encode(), first); out.Code != 409 {
		t.Fatal("disabled capture", out.Code)
	}
}
