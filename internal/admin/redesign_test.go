package admin

import (
	"crypto/tls"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNeutralHomeVisitor(t *testing.T) {
	c := config(t)
	s, err := OpenStore(c.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWeb(c, s)
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "198.51.100.23:54321"
	r.Header.Set("User-Agent", "<script>alert(1)</script>")
	r.Header.Set("X-Forwarded-For", "203.0.113.99")
	out := httptest.NewRecorder()
	w.Handler().ServeHTTP(out, r)
	body := out.Body.String()
	for _, want := range []string{"198.51.100.23", "Ваше подключение", "Вход администратора", "&lt;script&gt;"} {
		if !strings.Contains(body, want) {
			t.Errorf("home missing %q", want)
		}
	}
	for _, unwanted := range []string{"echo-стенд", "Echo / VPN", "203.0.113.99", "<script>alert(1)</script>"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("home contains %q", unwanted)
		}
	}
}

func TestUnifiedUserSettings(t *testing.T) {
	c := config(t)
	s, err := OpenStore(c.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateWithProtocols("original", []string{"quic"})
	if err != nil {
		t.Fatal(err)
	}
	w := NewWeb(c, s)
	h := w.Handler()
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	form := url.Values{"csrf": {w.sessions[cookie.Value].CSRF}, "id": {u.ID}, "name": {"renamed"}, "protocol": {"https"}}
	denied := call(h, "POST", "/users/settings", form.Encode(), nil)
	if denied.Code != 303 {
		t.Fatalf("unauthenticated: %d", denied.Code)
	}
	form.Set("name", "")
	bad := call(h, "POST", "/users/settings", form.Encode(), cookie)
	if bad.Code != 400 {
		t.Fatalf("invalid name: %d", bad.Code)
	}
	if got := s.List()[0]; got.Name != "original" || !got.QUICEnabled() || got.HTTPSEnabled() {
		t.Fatal("invalid form mutated user")
	}
	form.Set("name", "renamed")
	out := call(h, "POST", "/users/settings", form.Encode(), cookie)
	if out.Code != 303 {
		t.Fatalf("settings: %d %s", out.Code, out.Body.String())
	}
	got := s.List()[0]
	if got.Name != "renamed" || got.QUICEnabled() || !got.HTTPSEnabled() {
		t.Fatal("settings not saved together")
	}
}
func TestHomeTLSFacts(t *testing.T) {
	c := config(t)
	s, err := OpenStore(c.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	h := NewWeb(c, s).Handler()
	for _, tc := range []struct {
		name   string
		direct bool
		peer   string
		want   string
		not    string
	}{
		{"direct", true, "198.51.100.1:5000", "TLS 1.3", "TLSv1"},
		{"trusted legacy proxy", false, "127.0.0.1:5000", "TLSv1", "TLS 1.3"},
		{"spoofed proxy", false, "198.51.100.1:5000", "Не определено", "TLSv1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.peer
			if tc.direct {
				r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13, CipherSuite: tls.TLS_AES_128_GCM_SHA256, NegotiatedProtocol: "h2", ServerName: "lab.example"}
			}
			r.Header.Set("X-Quic-Lab-Peer", "203.0.113.20:51000")
			r.Header.Set("X-Portal-TLS-Version", "TLSv1")
			r.Header.Set("X-Portal-TLS-Cipher", "ECDHE-RSA-AES128-SHA")
			out := httptest.NewRecorder()
			h.ServeHTTP(out, r)
			body := out.Body.String()
			if !strings.Contains(body, tc.want) || strings.Contains(body, tc.not) {
				t.Fatalf("TLS facts do not respect trusted connection: want %s without %s", tc.want, tc.not)
			}
		})
	}
}
