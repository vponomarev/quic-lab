package admin

import (
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestSessionReloadRestartLogoutAndCredentials(t *testing.T) {
	c := config(t)
	s, err := OpenStore(c.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWeb(c, s)
	login := call(w.Handler(), "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	if login.Code != 303 || len(login.Result().Cookies()) != 1 {
		t.Fatal("login failed", login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	for i := 0; i < 2; i++ {
		if call(w.Handler(), "GET", "/users", "", cookie).Code != 200 {
			t.Fatal("reload lost session")
		}
	}
	w = NewWeb(c, s)
	if call(w.Handler(), "GET", "/users", "", cookie).Code != 200 {
		t.Fatal("restart lost session")
	}
	if got := call(w.Handler(), "GET", "/login", "", cookie); got.Code != 303 || got.Header().Get("Location") != "/admin/users" {
		t.Fatal("login did not redirect")
	}
	changed := c
	changed.Password += "changed"
	if call(NewWeb(changed, s).Handler(), "GET", "/users", "", cookie).Code != 303 {
		t.Fatal("password change retained session")
	}
	csrf := w.sessions[cookie.Value].CSRF
	if call(w.Handler(), "POST", "/logout", url.Values{"csrf": {csrf}}.Encode(), cookie).Code != 303 {
		t.Fatal("logout failed")
	}
	if call(NewWeb(c, s).Handler(), "GET", "/users", "", cookie).Code != 303 {
		t.Fatal("logout resurrected on restart")
	}
	w.sessions["expired"] = session{Until: time.Now().Add(-time.Second)}
	if err := w.saveSessions(); err != nil {
		t.Fatal(err)
	}
	w = NewWeb(c, s)
	if len(w.sessions) != 0 {
		t.Fatal("expired session restored")
	}
	r := httptest.NewRequest("GET", "/users", nil)
	if _, ok := w.getSession(r); ok {
		t.Fatal("anonymous session")
	}
}
