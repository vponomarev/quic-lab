package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"quiclab/internal/serverexit"
	"strings"
	"testing"
	"time"
)

func TestServerExitControlHTTP(t *testing.T) {
	s, w, d, token := updateFixture(t)
	s.ConfigureServerExits(exitDefinitions())
	s.SetServerExitApply(func([]string) error { return nil })
	grantExits(t, s, d.UserID, &serverexit.Policy{Allowed: []string{"west", "east"}, Default: "west"})
	request := func(method, auth, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/devices/"+d.ID+"/server-exit", strings.NewReader(body))
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		r.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		w.Handler().ServeHTTP(out, r)
		return out
	}
	if r := request("GET", "", ""); r.Code != 401 {
		t.Fatal("unauthenticated catalogue", r.Code)
	}
	r := request("GET", token, "")
	if r.Code != 200 || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(r.Code, r.Body.String())
	}
	var v DeviceServerExitView
	if err := json.Unmarshal(r.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"server_exit_id":"east","revision":"` + v.Revision + `","extra":true}`, `{} {}`, strings.Repeat("x", 20000)} {
		if r := request("POST", token, body); r.Code != 400 && r.Code != 413 {
			t.Fatal("invalid body accepted", r.Code)
		}
	}
	body := `{"server_exit_id":"east","revision":"` + v.Revision + `"}`
	if r := request("POST", token, body); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request("POST", token, body); r.Code != 409 {
		t.Fatal("stale POST", r.Code)
	}
	v, _ = s.DeviceServerExits(d.ID, token)
	if r := request("POST", token, `{"server_exit_id":"direct","revision":"`+v.Revision+`"}`); r.Code != 403 {
		t.Fatal("self-grant", r.Code)
	}
}

func TestServerExitAdminForm(t *testing.T) {
	s, w, d, _ := updateFixture(t)
	s.ConfigureServerExits(exitDefinitions())
	s.SetServerExitApply(func([]string) error { return nil })
	w.sessions["session"] = session{CSRF: "csrf", Until: time.Now().Add(time.Hour)}
	view, _ := s.UserServerExitPolicy(d.UserID)
	form := url.Values{"user_id": {d.UserID}, "revision": {view.Revision}, "allowed": {"west", "east"}, "default": {"west"}, "mode": {"managed"}, "csrf": {"csrf"}}
	r := httptest.NewRequest("POST", "/server-exits/policy", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "quiclab_admin", Value: "session"})
	out := httptest.NewRecorder()
	w.Handler().ServeHTTP(out, r)
	if out.Code != 403 {
		t.Fatal("origin not checked", out.Code)
	}
	r = httptest.NewRequest("POST", "/server-exits/policy", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", w.origin)
	r.AddCookie(&http.Cookie{Name: "quiclab_admin", Value: "session"})
	out = httptest.NewRecorder()
	w.Handler().ServeHTTP(out, r)
	if out.Code != 303 {
		t.Fatal(out.Code, out.Body.String())
	}
	r = httptest.NewRequest("GET", "/server-exits", nil)
	r.AddCookie(&http.Cookie{Name: "quiclab_admin", Value: "session"})
	out = httptest.NewRecorder()
	w.Handler().ServeHTTP(out, r)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "West") || strings.Contains(out.Body.String(), "PrivateKey") {
		t.Fatal("invalid admin page", out.Code)
	}
}
