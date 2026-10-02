package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"quiclab/internal/protocol"
)

func TestDirectRootDeviceConfigRouteReachesAdmin(t *testing.T) {
	const bearer = "Bearer test-token"
	admin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/devices/device-1/config" {
			t.Errorf("admin received path %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != bearer {
			t.Errorf("admin lost Authorization header: %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusNoContent)
	})
	h := withCapabilities(publicHandler(admin, "https://lab.example.org/lab/", slog.Default(), nil), protocol.DefaultCapabilities())
	r := httptest.NewRequest(http.MethodGet, "/api/v1/devices/device-1/config", nil)
	r.Header.Set("Authorization", bearer)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		body, _ := io.ReadAll(w.Body)
		t.Fatalf("root device config returned %d: %s", w.Code, body)
	}
}

func TestDirectRootAdminRoutesRemainHidden(t *testing.T) {
	called := false
	admin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	h := withCapabilities(publicHandler(admin, "https://lab.example.org/lab/", slog.Default(), nil), protocol.DefaultCapabilities())
	for _, path := range []string{"/api/v1/devices/device-1/other", "/api/v1/users"} {
		called = false
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound || called {
			t.Fatalf("admin path %s exposed: code=%d called=%t", path, w.Code, called)
		}
	}
}
