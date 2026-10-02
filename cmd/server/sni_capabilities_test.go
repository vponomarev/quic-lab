package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"quiclab/internal/protocol"
	"testing"
)

func TestPublicCapabilitiesWithoutClientCertificate(t *testing.T) {
	handler := withCapabilities(publicHandler(nil, "", slog.Default(), http.NotFoundHandler()), protocol.Capabilities{ControlVersion: 1, DataVersion: 7})
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/v1/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var got protocol.Capabilities
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || got.DataVersion != 7 {
		t.Fatalf("%d %+v", response.StatusCode, got)
	}
	response, err = server.Client().Get(server.URL + "/tunnel")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("tunnel admitted anonymous: %d", response.StatusCode)
	}
}

func TestSharedPublicSNIOnlyRestrictsTunnel(t *testing.T) {
	data := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := withCapabilities(publicHandler(nil, "", slog.Default(), vpnTunnelHandler(data, "real.test", []string{"cover.test"})), protocol.DefaultCapabilities())
	for _, test := range []struct {
		name, path string
		want       int
	}{{"real.test", "/tunnel", 204}, {"cover.test", "/tunnel", 204}, {"foreign.test", "/tunnel", 403}, {"foreign.test", "/api/v1/capabilities", 200}} {
		request := httptest.NewRequest("GET", test.path, nil)
		request.TLS = &tls.ConnectionState{ServerName: test.name, VerifiedChains: [][]*x509.Certificate{{{}}}}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("%s %s: %d", test.name, test.path, response.Code)
		}
	}
}
