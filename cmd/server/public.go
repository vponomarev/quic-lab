package main

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"quiclab/internal/echo"
	"quiclab/internal/gateway"
	"quiclab/internal/protocol"
	"quiclab/internal/servertls"
)

// Public routes accept anonymous TLS clients. Any supplied client certificate
// still requires chain validation and the original live revocation check.
func publicTLS(strict *tls.Config) *tls.Config {
	c := strict.Clone()
	c.ClientAuth = tls.VerifyClientCertIfGiven
	verify := strict.VerifyConnection
	c.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return nil
		}
		if verify != nil {
			return verify(cs)
		}
		return nil
	}
	return c
}

func publicHandler(admin http.Handler, adminURL string, log *slog.Logger, tunnel http.Handler, probes ...func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	if admin != nil {
		u, err := url.Parse(adminURL)
		if err != nil {
			panic(err)
		}
		base := u.Path
		mux.Handle(base, http.StripPrefix(strings.TrimSuffix(base, "/"), admin))
		if base != "/" {
			mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, base, http.StatusFound) })
		}
	}
	mux.Handle("/echo", echo.WebSocketHandler(log, probes...))
	mux.Handle("/vpn-demo/", http.StripPrefix("/vpn-demo", gateway.Demo(log)))
	if tunnel != nil {
		authorizedTunnel := clientCertificateRequired(tunnel)
		mux.Handle("/tunnel", authorizedTunnel)
		mux.Handle("/tunnel/bond", authorizedTunnel)
	}
	return mux
}

// Compatibility is public control metadata and must precede tunnel authorization.
func withCapabilities(next http.Handler, caps protocol.Capabilities) http.Handler {
	handler := protocol.CapabilitiesHandler(caps)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/capabilities" {
			handler.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientCertificateRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Authorization comes from the real TLS connection, never headers.
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			http.Error(w, "client certificate required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func vpnTunnelHandler(next http.Handler, realName string, covers []string) http.Handler {
	names := append([]string(nil), covers...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(names) > 0 && (r.TLS == nil || !servertls.ServerNameAllowed(realName, names, r.TLS.ServerName)) {
			http.Error(w, "VPN SNI not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
