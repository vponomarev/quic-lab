package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestLiveStatisticsAuthorizationUpdatesAndLogout(t *testing.T) {
	cfg := config(t)
	store, e := OpenStore(cfg.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	user, e := store.Create("Live phone")
	if e != nil {
		t.Fatal(e)
	}
	web := NewWeb(cfg, store)
	server := httptest.NewServer(web.Handler())
	defer server.Close()
	web.origin = server.URL
	web.sessions["live-test"] = session{CSRF: "test", Until: time.Now().Add(time.Hour)}
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/users/live"
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	dial := func(origin, cookie string) (*websocket.Conn, *http.Response, error) {
		h := http.Header{}
		h.Set("Origin", origin)
		if cookie != "" {
			h.Set("Cookie", "quiclab_admin="+cookie)
		}
		return websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h})
	}
	for _, test := range []struct {
		origin, cookie string
		code           int
	}{{"https://untrusted.example", "live-test", 403}, {server.URL, "", 401}, {server.URL, "missing", 401}, {"", "live-test", 403}} {
		c, r, e := dial(test.origin, test.cookie)
		if c != nil {
			c.CloseNow()
		}
		if e == nil || r == nil || r.StatusCode != test.code {
			t.Fatalf("authorization: %v %v", r, e)
		}
	}
	c, _, e := dial(server.URL, "live-test")
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	var first []liveUserStats
	if e = wsjson.Read(ctx, c, &first); e != nil {
		t.Fatal(e)
	}
	if len(first) != 1 || first[0].ID != user.ID || first[0].TX != "0 B" {
		t.Fatal(first)
	}
	started := time.Now()
	store.mu.Lock()
	store.stats = map[string]*userTraffic{user.ID: {live: make(map[string]liveConnection)}}
	store.stats[user.ID].add(time.Now(), 4096, 8192)
	store.mu.Unlock()
	var second []liveUserStats
	if e = wsjson.Read(ctx, c, &second); e != nil {
		t.Fatal(e)
	}
	if time.Since(started) < 4*time.Second || len(second) != 1 || second[0].TX != "4.0 KiB" || second[0].RX != "8.0 KiB" {
		t.Fatal(second)
	}
	web.mu.Lock()
	delete(web.sessions, "live-test")
	web.mu.Unlock()
	_, _, e = c.Read(ctx)
	if websocket.CloseStatus(e) != websocket.StatusPolicyViolation {
		t.Fatalf("logout must close socket: %v", e)
	}
}
