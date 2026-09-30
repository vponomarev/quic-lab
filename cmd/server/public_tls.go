package main

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
)

func publicTLSMinimum(v string) (uint16, error) {
	switch v {
	case "", "1.3":
		return tls.VersionTLS13, nil
	case "1.2":
		return tls.VersionTLS12, nil
	case "1.0":
		return tls.VersionTLS10, nil
	}
	return 0, fmt.Errorf("public_tls_min must be 1.0, 1.2 or 1.3")
}

// Applies only to the separate public listener; never relaxes QUIC or mTLS.
func configurePublicTLS(s *http.Server, opts serverConfig, log *slog.Logger) {
	s.TLSConfig.MinVersion, _ = publicTLSMinimum(opts.PublicTLSMin)
	if s.TLSConfig.MinVersion == tls.VersionTLS10 {
		// Keep forward secrecy. Legacy clients may use AES-CBC/SHA1 with ECDHE;
		// do not enable static RSA, RC4 or 3DES.
		s.TLSConfig.CipherSuites = []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256, tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
		}
	}
	log.Info("public_tls_policy", "min_version", tls.VersionName(s.TLSConfig.MinVersion), "diagnostics", opts.PublicTLSDiagnostics)
	if !opts.PublicTLSDiagnostics {
		return
	}
	s.TLSConfig.GetConfigForClient = func(h *tls.ClientHelloInfo) (*tls.Config, error) {
		versions := make([]string, 0, len(h.SupportedVersions))
		for _, v := range h.SupportedVersions {
			versions = append(versions, tls.VersionName(v))
		}
		suites := make([]string, 0, len(h.CipherSuites))
		for _, v := range h.CipherSuites {
			suites = append(suites, fmt.Sprintf("0x%04x:%s", v, tls.CipherSuiteName(v)))
		}
		log.Info("public_tls_client_hello", "remote", h.Conn.RemoteAddr().String(), "sni", h.ServerName, "versions", versions, "cipher_suites", suites, "alpn", h.SupportedProtos, "signature_schemes", h.SignatureSchemes, "groups", h.SupportedCurves)
		return nil, nil
	}
	s.ConnState = func(c net.Conn, state http.ConnState) {
		if state != http.StateActive && state != http.StateClosed && state != http.StateHijacked {
			return
		}
		t, ok := c.(*tls.Conn)
		if !ok {
			return
		}
		cs := t.ConnectionState()
		log.Info("public_tls_connection", "remote", c.RemoteAddr().String(), "state", state.String(), "handshake_complete", cs.HandshakeComplete, "version", tls.VersionName(cs.Version), "cipher_suite", tls.CipherSuiteName(cs.CipherSuite), "alpn", cs.NegotiatedProtocol, "resumed", cs.DidResume)
	}
	handler := s.Handler
	s.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never log query strings, cookies, authorization or dynamic enrollment/capture URLs.
		path := "(other)"
		switch r.URL.Path {
		case "/", "/some", "/lab/", "/lab/login", "/lab/users", "/echo":
			path = r.URL.Path
		}
		if r.TLS != nil {
			log.Info("public_https_request", "remote", r.RemoteAddr, "method", r.Method, "route", path, "version", tls.VersionName(r.TLS.Version), "cipher_suite", tls.CipherSuiteName(r.TLS.CipherSuite))
		}
		handler.ServeHTTP(w, r)
	})
}
