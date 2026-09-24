package admin

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUserTrafficWindowLifecycleAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.Create("Stats phone")
	if e != nil {
		t.Fatal(e)
	}
	block, _ := pem.Decode([]byte(u.Certificate))
	leaf, e := x509.ParseCertificate(block.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	cs := tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}, VerifiedChains: [][]*x509.Certificate{{leaf, s.ca}}}
	peer := "192.0.2.1:1234"
	count, done := s.Track(cs, "QUIC", func() string { return peer })
	count2, done2 := s.Track(cs, "HTTPS / WebSocket", func() string { return "[2001:db8::1]:4567" })
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				count(2, 3)
				count2(4, 5)
			}
		}()
	}
	wg.Wait()
	peer = "198.51.100.2:2345"
	got := s.List()[0]
	if got.Stats.TX != 6000 || got.Stats.RX != 8000 || len(got.Stats.Connections) != 2 {
		t.Fatalf("bad totals: %+v", got.Stats)
	}
	if got.Stats.Connections[0].Source != "198.51.100.2" || got.Stats.Connections[1].Source != "2001:db8::1" {
		t.Fatal("current source IP missing")
	}
	cfg := config(t)
	web := NewWeb(cfg, s)
	web.sessions["stats-session"] = session{CSRF: "x", Until: time.Now().Add(time.Hour)}
	page := call(web.Handler(), "GET", "/users", "", &http.Cookie{Name: "quiclab_admin", Value: "stats-session"})
	if page.Code != 200 || !strings.Contains(page.Body.String(), "198.51.100.2") || !strings.Contains(page.Body.String(), "5.9 KiB") || strings.Contains(page.Body.String(), "PRIVATE KEY") {
		t.Fatal("stats page rendering", page.Body.String())
	}
	done()
	done()
	done2()
	count(100, 100)
	got = s.List()[0]
	if len(got.Stats.Connections) != 0 || got.Stats.TX != 6000 {
		t.Fatal("closed session accounted", got.Stats)
	}
	reopened, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	saved := reopened.List()[0]
	if saved.LastConnected.IsZero() || saved.LastTransport != "HTTPS / WebSocket" || saved.LastSource != "2001:db8::1" || saved.Stats.TX != 0 {
		t.Fatal("metadata persistence", saved)
	}
	if e := s.Delete(u.ID); e != nil {
		t.Fatal(e)
	}
	count2(100, 100)
	if len(s.stats) != 0 {
		t.Fatal("deleted user statistics retained")
	}
	rejected, finish := s.Track(cs, "QUIC", func() string { return peer })
	finish()
	if rejected != nil {
		t.Fatal("revoked identity tracked")
	}
}
func TestTrafficWindowBoundary(t *testing.T) {
	now := time.Unix(1800000000, 0)
	s := &Store{stats: map[string]*userTraffic{"u": {}}}
	s.stats["u"].add(now.Add(-599*time.Second), 10, 20)
	s.stats["u"].add(now, 30, 40)
	if got := s.snapshot("u", now); got.TX != 40 || got.RX != 60 {
		t.Fatal(got)
	}
	if got := s.snapshot("u", now.Add(time.Second)); got.TX != 30 || got.RX != 40 {
		t.Fatal(got)
	}
	if got := s.snapshot("u", now.Add(600*time.Second)); got.TX != 0 || got.RX != 0 {
		t.Fatal(got)
	}
}
