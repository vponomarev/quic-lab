package admin

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenamePreservesIdentityAndConnections(t *testing.T) {
	dir := t.TempDir()
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.Create("Phone")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create("Taken"); e != nil {
		t.Fatal(e)
	}
	block, _ := pem.Decode([]byte(u.Certificate))
	leaf, e := x509.ParseCertificate(block.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	cs := tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}, VerifiedChains: [][]*x509.Certificate{{leaf, s.ca}}}
	closed := false
	release, e := s.Register(cs, func() { closed = true })
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	count, done := s.Track(cs, "QUIC", func() string { return "192.0.2.10:1234" })
	defer done()
	count(32, 64)
	if e = s.Rename(u.ID, "Renamed phone"); e != nil {
		t.Fatal(e)
	}
	got, e := s.Profile(u.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Name != "Renamed phone" || got.Certificate != u.Certificate || got.Key != u.Key || got.ID != u.ID || closed || s.Verify(cs) != nil {
		t.Fatal("rename changed identity or connection")
	}
	snapshot := NewWeb(config(t), s).statsSnapshot()
	for _, row := range snapshot {
		if row.ID == u.ID && (row.Name != got.Name || row.TX != "32 B" || len(row.Connections) != 1 || strings.Count(row.Last, "\n") != 1 || !strings.Contains(row.Last, "QUIC · 192.0.2.10")) {
			t.Fatal(row)
		}
	}
	for _, name := range []string{"", strings.Repeat("x", 101), "Taken"} {
		if s.Rename(u.ID, name) == nil {
			t.Fatal("invalid rename accepted")
		}
	}
	if s.Rename("missing", "Name") == nil {
		t.Fatal("missing user accepted")
	}
	oldPath := s.path
	s.path = filepath.Join(dir, "missing", "identities.json")
	if s.Rename(u.ID, "Unsaved") == nil {
		t.Fatal("save failure ignored")
	}
	s.path = oldPath
	got, _ = s.Profile(u.ID)
	if got.Name != "Renamed phone" {
		t.Fatal("rollback failed")
	}
	reopened, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	persisted, e := reopened.Profile(u.ID)
	if e != nil || persisted.Name != got.Name || persisted.Key != u.Key {
		t.Fatal("rename not persisted")
	}
}
func TestRenameHTTPAuthorizationAndEscaping(t *testing.T) {
	cfg := config(t)
	s, e := OpenStore(cfg.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.Create("Original")
	if e != nil {
		t.Fatal(e)
	}
	web := NewWeb(cfg, s)
	web.sessions["rename-test"] = session{CSRF: "token", Until: time.Now().Add(time.Hour)}
	h := web.Handler()
	cookie := &http.Cookie{Name: "quiclab_admin", Value: "rename-test"}
	form := url.Values{"csrf": {"token"}, "id": {u.ID}, "name": {"  <script>new name</script>  "}}
	if w := call(h, "POST", "/users/rename", form.Encode(), nil); w.Code != 303 {
		t.Fatal(w.Code)
	}
	if w := call(h, "POST", "/users/rename", "id="+u.ID+"&name=invalid", cookie); w.Code != 403 {
		t.Fatal("CSRF accepted")
	}
	if w := call(h, "POST", "/users/rename", form.Encode(), cookie); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	got, _ := s.Profile(u.ID)
	if got.Name != "<script>new name</script>" {
		t.Fatal(got.Name)
	}
	page := call(h, "GET", "/users", "", cookie).Body.String()
	if strings.Contains(page, "<script>new name</script>") || !strings.Contains(page, "&lt;script&gt;new name&lt;/script&gt;") {
		t.Fatal("name not escaped")
	}
	form.Set("name", " ")
	if w := call(h, "POST", "/users/rename", form.Encode(), cookie); w.Code != 400 {
		t.Fatal("blank rename accepted")
	}
}
