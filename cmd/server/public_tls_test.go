package main

import (
	"bytes"
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicTLSCompatibility(t *testing.T) {
	for _, minimum := range []string{"", "1.2", "1.0"} {
		for _, version := range []uint16{tls.VersionTLS10, tls.VersionTLS12, tls.VersionTLS13} {
			t.Run(minimum+"/"+tls.VersionName(version), func(t *testing.T) {
				var logs bytes.Buffer
				s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
				s.Config.TLSConfig = &tls.Config{}
				configurePublicTLS(s.Config, serverConfig{PublicTLSMin: minimum, PublicTLSDiagnostics: true}, slog.New(slog.NewJSONHandler(&logs, nil)))
				s.TLS = s.Config.TLSConfig
				s.StartTLS()
				defer s.Close()
				tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: version, MaxVersion: version, CipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}}}
				defer tr.CloseIdleConnections()
				client := &http.Client{Transport: tr}
				resp, err := client.Get(s.URL + "/some?secret=DO_NOT_LOG")
				min, _ := publicTLSMinimum(minimum)
				if version < min {
					if err == nil {
						resp.Body.Close()
						t.Fatal("accepted disallowed version")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				tr.CloseIdleConnections()
				s.CloseClientConnections()
				s.Close()
				if resp.StatusCode != 204 || resp.TLS.Version != version {
					t.Fatal("unexpected response")
				}
				data := logs.String()
				if !strings.Contains(data, "public_tls_client_hello") || !strings.Contains(data, "public_https_request") {
					t.Fatal(data)
				}
				if strings.Contains(data, "DO_NOT_LOG") {
					t.Fatal("query leaked")
				}
			})
		}
	}
}
func TestPublicTLSConfigIsolation(t *testing.T) {
	for _, args := range [][]string{
		{"-ephemeral-cert", "-public-tls-min", "1.1"},
		{"-ephemeral-cert", "-public-tls-min", "1.0"},
		{"-ephemeral-cert", "-public-tls-min", "1.0", "-https-listen", ":443", "-gateway-https", ":443"},
	} {
		if _, err := parseServerConfig(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if _, err := parseServerConfig([]string{"-ephemeral-cert", "-https-listen", ":443", "-public-tls-min", "1.0", "-public-tls-diagnostics"}); err != nil {
		t.Fatal(err)
	}
}
