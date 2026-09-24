package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"
	"quiclab/internal/admin"
	"quiclab/internal/echo"
	"quiclab/internal/gateway"
	"quiclab/internal/labcert"
	"quiclab/internal/protocol"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:4433", "UDP listen address")
	certPath := flag.String("cert", "", "PEM certificate file")
	keyPath := flag.String("key", "", "PEM private key file")
	ephemeral := flag.Bool("ephemeral-cert", false, "generate an in-memory lab certificate; pin printed SHA-256 on client")
	webListen := flag.String("web-listen", "", "optional loopback HTTP WebSocket endpoint behind nginx")
	gatewayQUIC := flag.String("gateway-quic", "", "optional authenticated gateway UDP address, e.g. :4434")
	gatewayHTTPS := flag.String("gateway-https", "", "optional direct mTLS HTTPS address, e.g. :8443")
	clientCA := flag.String("client-ca", "", "trusted client CA PEM; required for gateway")
	allow := flag.String("gateway-allow", "", "required comma-separated IPv4 destination CIDRs")
	demoListen := flag.String("demo-listen", "", "optional HTTP download demo, bind loopback behind nginx")
	adminFile := flag.String("admin-config", "", "optional admin JSON config; enables managed client CA")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	var cert tls.Certificate
	var err error
	if *ephemeral && *certPath == "" && *keyPath == "" {
		var cp, kp []byte
		cp, kp, err = labcert.Generate()
		if err == nil {
			cert, err = tls.X509KeyPair(cp, kp)
		}
	} else if !*ephemeral && *certPath != "" && *keyPath != "" {
		cert, err = tls.LoadX509KeyPair(*certPath, *keyPath)
	} else {
		log.Error("choose -ephemeral-cert OR both -cert and -key")
		os.Exit(1)
	}
	if err != nil {
		log.Error("certificate", "error", err)
		os.Exit(1)
	}
	ln, err := quic.ListenAddr(*addr, &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{protocol.ALPN}, MinVersion: tls.VersionTLS13},
		&quic.Config{MaxIdleTimeout: 90 * time.Second, MaxIncomingStreams: 1, MaxIncomingUniStreams: -1})
	if err != nil {
		log.Error("listen", "error", err)
		os.Exit(1)
	}
	defer ln.Close()
	log.Info("listening", "address", ln.Addr().String(), "certificate_sha256", labcert.Fingerprint(cert))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var managed *admin.Store
	if *adminFile != "" {
		cfg, e := admin.ReadConfig(*adminFile)
		if e != nil {
			log.Error("admin_config", "error", e)
			os.Exit(1)
		}
		managed, e = admin.OpenStore(cfg.DataDir)
		if e != nil {
			log.Error("admin_store", "error", e)
			os.Exit(1)
		}
		ui := admin.NewWeb(cfg, managed)
		as := &http.Server{Addr: cfg.Listen, Handler: ui.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
		defer as.Close()
		go func() {
			if e := as.ListenAndServe(); e != nil && e != http.ErrServerClosed {
				log.Error("admin_listen", "error", e)
				cancel()
			}
		}()
	}
	if *gatewayQUIC != "" || *gatewayHTTPS != "" {
		gw, e := gateway.New(*allow, log)
		if e != nil {
			log.Error("gateway_policy", "error", e)
			os.Exit(1)
		}
		var mtls *tls.Config
		if managed != nil {
			mtls = managed.TLS(cert)
			mtls.NextProtos = []string{gateway.ALPN}
			gw.Register = managed.Register
		} else {
			mtls, e = gateway.TLS(cert, *clientCA)
		}
		if e != nil {
			log.Error("gateway_tls", "error", e)
			os.Exit(1)
		}
		if *gatewayQUIC != "" {
			ql, e := quic.ListenAddr(*gatewayQUIC, mtls, &quic.Config{MaxIdleTimeout: 90 * time.Second, KeepAlivePeriod: 2 * time.Second, MaxIncomingStreams: gateway.MaxFlows, MaxIncomingUniStreams: -1})
			if e != nil {
				log.Error("gateway_listen", "error", e)
				os.Exit(1)
			}
			defer ql.Close()
			go func() {
				if e := gw.ServeQUIC(ctx, ql); e != nil && ctx.Err() == nil {
					log.Error("gateway_quic", "error", e)
					cancel()
				}
			}()
		}
		if *gatewayHTTPS != "" {
			tc := mtls.Clone()
			tc.NextProtos = []string{"http/1.1"}
			mux := http.NewServeMux()
			mux.HandleFunc("/tunnel", gw.WebSocket)
			hs := &http.Server{Addr: *gatewayHTTPS, Handler: mux, TLSConfig: tc, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16384}
			defer hs.Close()
			go func() {
				if e := hs.ListenAndServeTLS("", ""); e != nil && e != http.ErrServerClosed {
					log.Error("gateway_https", "error", e)
					cancel()
				}
			}()
		}
	}
	if *demoListen != "" {
		ds := &http.Server{Addr: *demoListen, Handler: gateway.Demo(log), ReadHeaderTimeout: 5 * time.Second}
		defer ds.Close()
		go func() {
			if e := ds.ListenAndServe(); e != nil && e != http.ErrServerClosed {
				log.Error("demo", "error", e)
				cancel()
			}
		}()
	}
	if *webListen != "" {
		mux := http.NewServeMux()
		mux.Handle("/echo", echo.WebSocketHandler(log))
		srv := &http.Server{Addr: *webListen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second}
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error("websocket_listen", "error", err)
				cancel()
			}
		}()
		defer srv.Close()
	}
	if err := echo.Serve(ctx, ln, log); err != nil && ctx.Err() == nil {
		log.Error("serve", "error", err)
		os.Exit(1)
	}
}
