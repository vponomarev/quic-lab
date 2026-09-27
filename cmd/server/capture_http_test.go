package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"quiclab/internal/admin"
	"quiclab/internal/debugcapture"
	"quiclab/internal/labcert"
)

func TestCaptureHTTPFlow(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("QUICLAB_CAPTURE_INTEGRATION") != "1" {
		t.Skip("opt-in Linux tcpdump integration")
	}
	cp, kp, _ := labcert.Generate()
	cert, e := tls.X509KeyPair(cp, kp)
	if e != nil {
		t.Fatal(e)
	}
	cfg := admin.Config{PublicURL: "https://lab.example/lab/", DataDir: t.TempDir(), Username: "teacher", Password: "synthetic-test-password", Echo: admin.Profile{Hostname: "lab.example"}}
	store, e := admin.OpenStore(cfg.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dump, _ := exec.LookPath("tcpdump")
	manager, e := debugcapture.New(ctx, debugcapture.Config{Interface: "lo", FirstPort: 59442, Slots: 2, TCPDump: dump}, captureFactory(cert, cfg, store, "0.0.0.0/0", slog.New(slog.NewTextHandler(io.Discard, nil))))
	if e != nil {
		t.Fatal(e)
	}
	web := admin.NewWeb(cfg, store)
	web.Capture = manager
	server := httptest.NewTLSServer(web.Handler())
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	var cookie *http.Cookie
	request := func(method, path, body string) (int, string) {
		t.Helper()
		r, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		r.Header.Set("Origin", "https://lab.example")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		resp, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if path == "/login" && len(resp.Cookies()) > 0 {
			cookie = resp.Cookies()[0]
		}
		return resp.StatusCode, string(b)
	}
	if code, _ := request("POST", "/login", url.Values{"username": {cfg.Username}, "password": {cfg.Password}}.Encode()); code != 303 {
		t.Fatal("login", code)
	}
	code, page := request("GET", "/capture", "")
	if code != 200 {
		t.Fatal(code)
	}
	extract := func(pattern, text string) string {
		t.Helper()
		v := regexp.MustCompile(pattern).FindStringSubmatch(text)
		if len(v) < 2 {
			t.Fatalf("missing field %s", pattern)
		}
		return html.UnescapeString(v[1])
	}
	csrf := extract(`name="csrf" value="([^"]+)"`, page)
	form := url.Values{"csrf": {csrf}, "kind": {"echo"}}
	if code, page = request("POST", "/capture/start", form.Encode()); code != 303 {
		t.Fatalf("start %d %s", code, page)
	}
	_, page = request("GET", "/capture", "")
	id := extract(`name="id" value="([^"]+)"`, page)
	form.Set("id", id)
	code, _ = request("POST", "/capture/qr", form.Encode())
	if code != 410 {
		t.Fatal("client QR must be retired")
	}
	_, page = request("POST", "/capture/launch", form.Encode())
	uri, _ := url.Parse(extract(`href="(quic-lab:[^"]+)"`, page))
	payload, _ := json.Marshal(map[string]string{"id": id, "ticket": uri.Fragment})
	resp, e := client.Post(server.URL+"/capture/redeem", "application/json", strings.NewReader(string(payload)))
	if e != nil {
		t.Fatal(e)
	}
	var auth struct {
		Token string
		Port  int
	}
	json.NewDecoder(resp.Body).Decode(&auth)
	resp.Body.Close()
	if resp.StatusCode != 200 || auth.Port != 59442 || auth.Token == "" {
		t.Fatal("redeem", resp.StatusCode, auth.Port)
	}
	replay, _ := client.Post(server.URL+"/capture/redeem", "application/json", strings.NewReader(string(payload)))
	replay.Body.Close()
	if replay.StatusCode != 410 {
		t.Fatal("launch ticket replay")
	}
	conn, _, e := websocket.Dial(ctx, "wss"+strings.TrimPrefix(server.URL, "https")+"/capture/stream?id="+id, &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Authorization": {"Bearer " + auth.Token}}})
	if e != nil {
		t.Fatal(e)
	}
	defer conn.CloseNow()
	// Must stream a valid section immediately, before any phone traffic.
	kind, head, e := conn.Read(ctx)
	if e != nil || kind != websocket.MessageBinary || len(head) < 28 || head[0] != 0x0a {
		t.Fatal("stream header", e)
	}
	// A separate admin login must not access the first teacher's capture.
	ownerCookie := cookie
	request("POST", "/login", url.Values{"username": {cfg.Username}, "password": {cfg.Password}}.Encode())
	if code, _ = request("GET", "/capture/download?id="+id, ""); code != 404 {
		t.Fatal("cross-login download", code)
	}
	cookie = ownerCookie
	if code, _ = request("POST", "/capture/stop", form.Encode()); code != 303 {
		t.Fatal("stop", code)
	}
	for {
		_, _, e = conn.Read(ctx)
		if e != nil {
			break
		}
	}
	if websocket.CloseStatus(e) != websocket.StatusNormalClosure {
		t.Fatal("unclean stream stop", e)
	}
	code, data := request("GET", "/capture/download?id="+id, "")
	if code != 200 || len(data) < 28 {
		t.Fatal("download", code)
	}
}
