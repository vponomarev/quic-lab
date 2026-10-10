package editor

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDeviceHTTP(t *testing.T) {
	s := NewStore(time.Now)
	a, _ := s.Create("owner", 30*time.Minute)
	h := NewHandler(s)
	call := func(path, token, body string, tlsOn bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		if tlsOn {
			r.TLS = &tls.ConnectionState{Version: tls.VersionTLS12}
		}
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body, _ := json.Marshal(map[string]any{"id": a.ID, "generation": 1, "configuration": document})
	if w := call("/api/editor/v1/pair", a.PairToken, string(body), false); w.Code != 426 {
		t.Fatal(w.Code)
	}
	w := call("/api/editor/v1/pair", a.PairToken, string(body), true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var g Grant
	if json.Unmarshal(w.Body.Bytes(), &g) != nil || g.DeviceToken == "" {
		t.Fatal("grant")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache")
	}
	poll := `{"id":"` + a.ID + `"}`
	if w := call("/api/editor/v1/poll", a.BrowserToken, poll, true); w.Code != 401 {
		t.Fatal("role", w.Code)
	}
	if w := call("/api/editor/v1/poll", g.DeviceToken, poll, true); w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, bad := range []string{`{"id":"x","id":"y"}`, `{"id":null}`, poll + `{}`, `{"id":"x","unknown":1}`} {
		if w := call("/api/editor/v1/poll", g.DeviceToken, bad, true); w.Code != 400 {
			t.Fatal("invalid", w.Code)
		}
	}
	if w := call("/api/editor/v1/poll?token=secret", g.DeviceToken, poll, true); w.Code != 400 {
		t.Fatal("query", w.Code)
	}
	if w := call("/api/editor/v1/create", g.DeviceToken, poll, true); w.Code != 404 {
		t.Fatal("admin API exposed", w.Code)
	}
	if w := call("/api/editor/v1/poll", g.DeviceToken, string(bytes.Repeat([]byte("x"), 2<<20)), true); w.Code != 413 {
		t.Fatal("size", w.Code)
	}
	if w := call("/api/editor/v1/revoke", g.DeviceToken, poll, true); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := call("/api/editor/v1/poll", g.DeviceToken, poll, true); w.Code != 401 {
		t.Fatal(w.Code)
	}
}
