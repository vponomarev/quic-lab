package probeecho

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeEchoContract(t *testing.T) {
	h := NewHandler()
	for _, tc := range []struct {
		method, body string
		code         int
	}{
		{"POST", `{"version":1,"nonce":"` + strings.Repeat("ab", 32) + `"}`, 200},
		{"GET", "", 405}, {"POST", `{"version":1,"nonce":"short"}`, 400},
		{"POST", `{"version":2,"nonce":"` + strings.Repeat("ab", 32) + `"}`, 400},
		{"POST", strings.Repeat("x", 1025), 400},
		{"POST", `{"version":1,"nonce":"` + strings.Repeat("ab", 32) + `","target":"https://example.org"}`, 400},
	} {
		r := httptest.NewRequest(tc.method, "/api/v1/probe/echo", strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s got %d want %d", tc.body, w.Code, tc.code)
		}
		if tc.code == 200 {
			var v struct {
				Version int
				Nonce   string
			}
			if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Version != 1 || v.Nonce != strings.Repeat("ab", 32) || w.Body.Len() > 1024 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("invalid echo", w.Body.String())
			}
		}
	}
}
func TestProbeEchoRateBound(t *testing.T) {
	h := NewHandler()
	limited := false
	for i := 0; i < 200; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"version":1,"nonce":"`+strings.Repeat("ab", 32)+`"}`)))
		if w.Code == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("unbounded anonymous traffic")
	}
}
