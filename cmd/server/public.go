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
		mux.Handle("/tunnel", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Authorization comes only from the real TLS connection, never headers.
			if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
				http.Error(w, "client certificate required", http.StatusForbidden)
				return
			}
			tunnel.ServeHTTP(w, r)
		}))
	}
	return mux
}
