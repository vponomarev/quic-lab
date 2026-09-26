package admin

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func config(t *testing.T) Config {
	return Config{Listen: "127.0.0.1:8083", PublicURL: "https://lab.example/admin/", Username: "teacher", Password: "test-password-not-production", DataDir: t.TempDir(), Echo: Profile{Endpoint: "lab.example:4433", Hostname: "lab.example"}, VPN: Profile{QUIC: "lab.example:4434", HTTPS: "lab.example:8443", Hostname: "lab.example", DNS: "1.1.1.1"}}
}
func call(h http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://lab.example"+path, strings.NewReader(body))
	r.Header.Set("Origin", "https://lab.example")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestWebEnrollmentAndRevocation(t *testing.T) {
	c := config(t)
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	s, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	web := NewWeb(c, s)
	h := web.Handler()
	pub := call(h, "GET", "/", "", nil)
	if pub.Code != 200 || !strings.Contains(pub.Body.String(), "data:image/png;base64,") || strings.Contains(pub.Body.String(), "PRIVATE KEY") {
		t.Fatal("public echo page")
	}
	if w := call(h, "GET", "/users", "", nil); w.Code != 303 {
		t.Fatal("admin leaked")
	}
	if w := call(h, "POST", "/login", "username=teacher&password=bad", nil); w.Code != 401 {
		t.Fatal("invalid password accepted")
	}
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookies := login.Result().Cookies()
	if login.Code != 303 || len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("login cookie")
	}
	cookie := cookies[0]
	session := web.sessions[cookie.Value]
	form := url.Values{"csrf": {session.CSRF}, "name": {"<script>phone</script>"}}
	if w := call(h, "POST", "/users/add", "name=bad", cookie); w.Code != 403 {
		t.Fatal("CSRF bypass")
	}
	if w := call(h, "POST", "/users/add", form.Encode(), cookie); w.Code != 303 {
		t.Fatal(w.Body.String())
	}
	list := s.List()
	if len(list) != 1 {
		t.Fatal(list)
	}
	id := list[0].ID
	page := call(h, "GET", "/users", "", cookie)
	if strings.Contains(page.Body.String(), "<script>phone</script>") {
		t.Fatal("HTML injection")
	}
	form.Set("id", id)
	qr := call(h, "POST", "/users/qr", form.Encode(), cookie)
	if qr.Code != 200 || !strings.Contains(qr.Body.String(), "data:image/png;base64,") {
		t.Fatal("QR missing")
	}
	var token string
	for k := range web.tickets {
		token = k
	}
	body := `{"token":"` + token + `"}`
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := call(h, "POST", "/enroll", body, nil)
			if r.Code == 200 {
				success.Add(1)
				var p Profile
				if json.Unmarshal(r.Body.Bytes(), &p) != nil || p.Kind != "vpn" || p.Key == "" || p.QUIC != c.VPN.QUIC {
					t.Error("bad profile")
				}
			} else if r.Code != 410 {
				t.Errorf("redeem code %d", r.Code)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatal("token not single-use")
	}
	u, _ := s.Profile(id)
	block, _ := pem.Decode([]byte(u.Certificate))
	leaf, _ := x509.ParseCertificate(block.Bytes)
	cs := tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}, VerifiedChains: [][]*x509.Certificate{{leaf, s.ca}}}
	closed := make(chan struct{}, 1)
	release, e := s.Register(cs, func() { closed <- struct{}{} })
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	if w := call(h, "POST", "/users/delete", form.Encode(), cookie); w.Code != 303 {
		t.Fatal(w.Body.String())
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("active connection not revoked")
	}
	if s.Verify(cs) == nil {
		t.Fatal("deleted client authorized")
	}
	reopened, e := OpenStore(c.DataDir)
	if e != nil || len(reopened.List()) != 0 {
		t.Fatal("deletion not persisted", e)
	}
	if _, e = os.Stat(filepath.Join(c.DataDir, "identities.json")); e != nil {
		t.Fatal(e)
	}
}
func TestExpiredAndSupersededQR(t *testing.T) {
	c := config(t)
	s, _ := OpenStore(c.DataDir)
	u, _ := s.Create("phone")
	w := NewWeb(c, s)
	w.tickets["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"] = ticket{u.ID, time.Now().Add(-time.Second)}
	r := call(w.Handler(), "POST", "/enroll", `{"token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, nil)
	if r.Code != 410 {
		t.Fatal(r.Code)
	}
	reopened, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	saved, e := reopened.Profile(u.ID)
	if e != nil || saved.Key != u.Key {
		t.Fatal("identity not persisted")
	}
}

func TestLoginOriginPolicy(t *testing.T) {
	c := config(t)
	store, err := OpenStore(c.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	h := NewWeb(c, store).Handler()
	page := call(h, "GET", "/login", "", nil)
	if page.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatal("form POST must preserve same-origin Origin")
	}
	for _, origin := range []string{"", "null", "https://evil.example", "https://lab.example"} {
		r := httptest.NewRequest("POST", "https://lab.example/login", strings.NewReader(url.Values{"username": {c.Username}, "password": {c.Password}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 403
		if origin == "https://lab.example" {
			want = 303
		}
		if w.Code != want {
			t.Fatalf("origin %q: got %d, want %d", origin, w.Code, want)
		}
	}
}
