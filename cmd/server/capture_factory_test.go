package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/quic-go/quic-go"
	"quiclab/internal/admin"
	"quiclab/internal/debugcapture"
	"quiclab/internal/echo"
	"quiclab/internal/gateway"
	"quiclab/internal/protocol"
)

func captureFactory(cert tls.Certificate, cfg admin.Config, store *admin.Store, allow string, log *slog.Logger) debugcapture.Factory {
	return func(s *debugcapture.Session) (func(), error) {
		tc := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13, NextProtos: []string{protocol.ALPN}, KeyLogWriter: s, SessionTicketsDisabled: true}
		var gw *gateway.Server
		var handler http.Handler
		if s.Kind == "vpn" {
			user, e := store.Profile(s.User)
			if e != nil {
				return nil, e
			}
			if !user.Allows("quic") && !user.Allows("https") {
				return nil, errors.New("user needs QUIC or HTTPS access")
			}
			tc = store.TLS(cert)
			tc.NextProtos = []string{gateway.ALPN}
			tc.KeyLogWriter = s
			tc.SessionTicketsDisabled = true
			verify := tc.VerifyConnection
			tc.VerifyConnection = func(cs tls.ConnectionState) error {
				if e := verify(cs); e != nil {
					return e
				}
				if len(cs.PeerCertificates) == 0 || cs.PeerCertificates[0].Subject.CommonName != user.ID {
					return errors.New("debug profile belongs to another user")
				}
				return nil
			}
			gw, e = gateway.New(allow, log)
			if e != nil {
				return nil, e
			}
			gw.RegisterProtocol = store.RegisterProtocol
			gw.Track = store.Track
			if cfg.Transit != nil {
				gw.DialContext = cfg.Transit.DialContext
				gw.Probe = cfg.Transit.Probe
				gw.Resolver = cfg.Transit.Resolver()
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/tunnel", gw.WebSocket)
			handler = mux
		} else {
			mux := http.NewServeMux()
			if cfg.Transit != nil {
				mux.Handle("/echo", echo.WebSocketHandler(log, cfg.Transit.Probe))
			} else {
				mux.Handle("/echo", echo.WebSocketHandler(log))
			}
			handler = mux
		}
		addr := fmt.Sprintf("0.0.0.0:%d", s.Port)
		tcp, e := net.Listen("tcp4", addr)
		if e != nil {
			return nil, e
		}
		udp, e := quic.ListenAddr(addr, tc, &quic.Config{EnableDatagrams: s.Kind == "vpn", MaxIdleTimeout: 90 * time.Second, KeepAlivePeriod: 2 * time.Second, MaxIncomingStreams: gateway.MaxFlows, MaxIncomingUniStreams: -1})
		if e != nil {
			tcp.Close()
			return nil, e
		}
		httpsTLS := tc.Clone()
		httpsTLS.NextProtos = []string{"http/1.1"}
		hs := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16384}
		// WebSockets are hijacked HTTP connections: Close alone does not close them.
		handlerWithCancel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r = r.WithContext(s.Context()); handler.ServeHTTP(w, r) })
		hs.Handler = handlerWithCancel
		go func() {
			e := hs.Serve(tls.NewListener(tcp, httpsTLS))
			if e != nil && s.Context().Err() == nil {
				s.Stop("HTTPS listener stopped")
			}
		}()
		go func() {
			if gw != nil {
				_ = gw.ServeQUIC(s.Context(), udp)
			} else if cfg.Transit != nil {
				_ = echo.Serve(s.Context(), udp, log, cfg.Transit.Probe)
			} else {
				_ = echo.Serve(s.Context(), udp, log)
			}
			if s.Context().Err() == nil {
				s.Stop("QUIC listener stopped")
			}
		}()
		log.Info("debug_capture_started", "id", s.ID, "kind", s.Kind, "port", s.Port, "until", s.Until)
		return func() { udp.Close(); hs.Close(); tcp.Close(); log.Info("debug_capture_stopped", "id", s.ID) }, nil
	}
}
