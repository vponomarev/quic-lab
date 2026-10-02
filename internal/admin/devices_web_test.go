package admin

import (
	"net/url"
	"strings"
	"testing"
)

func TestDeviceAdminGroupingAndRevocation(t *testing.T) {
	c := config(t)
	s, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.CreateWithProtocols("owner", []string{"quic"})
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.CreateDevice(u.ID, "second phone")
	if e != nil {
		t.Fatal(e)
	}
	w := NewWeb(c, s)
	h := w.Handler()
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	page := call(h, "GET", "/users", "", cookie)
	if !strings.Contains(page.Body.String(), "second phone") || !strings.Contains(page.Body.String(), "Legacy device") {
		t.Fatal("devices not grouped in admin")
	}
	if strings.Contains(page.Body.String(), d.Key) || strings.Contains(page.Body.String(), "PRIVATE KEY") {
		t.Fatal("admin devices leaked secrets")
	}
	form := url.Values{"csrf": {w.sessions[cookie.Value].CSRF}, "id": {d.ID}}
	response := call(h, "POST", "/devices/disable", form.Encode(), cookie)
	if response.Code != 303 {
		t.Fatal("device revoke route", response.Code, response.Body.String())
	}
	if s.Verify(deviceTLS(t, d.Certificate)) == nil || s.Verify(deviceTLS(t, u.Certificate)) != nil {
		t.Fatal("admin revoke affected wrong identity")
	}
}
