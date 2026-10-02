// quic-lab-demux is the standalone, mTLS-authenticated session exit.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/quic-go/quic-go"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"quiclab/internal/bond"
	"quiclab/internal/gateway"
	"quiclab/internal/protocol"
	"quiclab/internal/servertls"
	"syscall"
	"time"
)

type config struct {
	TLSHost                    string                `json:"tls_host,omitempty"`
	VPNSNINames                []string              `json:"vpn_sni_names,omitempty"`
	Capabilities               protocol.Capabilities `json:"capabilities,omitempty"`
	HTTPSListen                string                `json:"https_listen,omitempty"`
	BondDisconnectGraceSeconds int                   `json:"bond_disconnect_grace_seconds,omitempty"`
	Listen                     string                `json:"listen"`
	Cert                       string                `json:"cert"`
	Key                        string                `json:"key"`
	ClientCA                   string                `json:"client_ca"`
	Allow                      string                `json:"allow"`
}

func run() error {
	path := flag.String("config", "", "JSON configuration file")
	flag.Parse()
	if *path == "" {
		flag.Usage()
		return errors.New("-config is required")
	}
	f, e := os.Open(*path)
	if e != nil {
		return e
	}
	defer f.Close()
	cfg := config{Capabilities: protocol.DefaultCapabilities()}
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if e = d.Decode(&cfg); e != nil {
		return e
	}
	if cfg.Listen == "" || cfg.Cert == "" || cfg.Key == "" || cfg.ClientCA == "" || cfg.Allow == "" {
		return errors.New("listen, cert, key, client_ca and allow are required")
	}
	if cfg.BondDisconnectGraceSeconds < 0 || cfg.BondDisconnectGraceSeconds > 3600 {
		return errors.New("bond_disconnect_grace_seconds must be 0..3600")
	}
	cert, e := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
	if e != nil {
		return e
	}
	tc, e := gateway.TLS(cert, cfg.ClientCA)
	if e != nil {
		return e
	}
	tc.NextProtos = []string{gateway.BondALPN}
	if len(cfg.VPNSNINames) > 0 && cfg.TLSHost == "" {
		return errors.New("vpn_sni_names requires real tls_host")
	}
	tc = servertls.AllowServerNames(tc, cfg.TLSHost, cfg.VPNSNINames)
	// Certificate renewal is picked up on the next handshake. Existing sessions persist.
	tc.Certificates = nil
	tc.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		c, e := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
		return &c, e
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	srv, e := gateway.New(cfg.Allow, log)
	if e != nil {
		return e
	}
	srv.BondOptions = bond.Options{DisconnectGrace: time.Duration(cfg.BondDisconnectGraceSeconds) * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv.BondContext = ctx
	if cfg.HTTPSListen != "" {
		listener, err := net.Listen("tcp", cfg.HTTPSListen)
		if err != nil {
			return err
		}
		httpsTLS := tc.Clone()
		httpsTLS.NextProtos = []string{"http/1.1"}
		httpsTLS.ClientAuth = tls.VerifyClientCertIfGiven
		hs := &http.Server{Handler: demuxHandler(http.HandlerFunc(srv.ServeBondHTTPS), cfg.Capabilities), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16384}
		defer hs.Close()
		stopHTTP := context.AfterFunc(ctx, func() { hs.Close() })
		defer stopHTTP()
		go func() {
			if err := hs.Serve(tls.NewListener(listener, httpsTLS)); err != nil && err != http.ErrServerClosed {
				log.Error("demux_https", "error", err)
				stop()
			}
		}()
	}
	ln, e := quic.ListenAddr(cfg.Listen, tc, &quic.Config{EnableDatagrams: true, MaxIncomingStreams: 1, MaxIncomingUniStreams: -1, MaxIdleTimeout: 10 * time.Second})
	if e != nil {
		return e
	}
	defer ln.Close()
	go func() { <-ctx.Done(); ln.Close() }()
	log.Info("demux_listening", "address", ln.Addr().String())
	e = srv.ServeQUIC(ctx, ln)
	if ctx.Err() != nil {
		return nil
	}
	return e
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

func demuxHandler(data http.Handler, caps protocol.Capabilities) http.Handler {
	control := protocol.CapabilitiesHandler(caps)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/capabilities" {
			control.ServeHTTP(w, r)
			return
		}
		if r.URL.Path != "/tunnel/bond" {
			http.NotFound(w, r)
			return
		}
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			http.Error(w, "client certificate required", http.StatusForbidden)
			return
		}
		data.ServeHTTP(w, r)
	})
}
