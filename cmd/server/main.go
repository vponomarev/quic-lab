package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"
	"path/filepath"
	"quiclab/internal/admin"
	"quiclab/internal/awgserver"
	"quiclab/internal/bond"
	"quiclab/internal/debugcapture"
	"quiclab/internal/echo"
	"quiclab/internal/echoawg"
	"quiclab/internal/gateway"
	"quiclab/internal/labcert"
	"quiclab/internal/protocol"
	"quiclab/internal/servertls"
	"quiclab/internal/transit"
	"quiclab/internal/vlessserver"
)

func main() {
	opts, err := parseServerConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	addr, certPath, keyPath := &opts.Listen, &opts.Cert, &opts.Key
	ephemeral, fallback, tlsHost := &opts.EphemeralCert, &opts.TLSFallback, &opts.TLSHost
	publicHTTPS, webListen := &opts.HTTPSListen, &opts.WebListen
	gatewayQUIC, gatewayHTTPS := &opts.GatewayQUIC, &opts.GatewayHTTPS
	clientCA, allow := &opts.ClientCA, &opts.GatewayAllow
	demoListen, adminFile := &opts.DemoListen, &opts.AdminConfig
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	var cert tls.Certificate
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
	reloadable := labcert.NewReloadable(cert)
	keys := new(debugcapture.Router)
	echoTLS := &tls.Config{GetCertificate: reloadable.GetCertificate, KeyLogWriter: keys.Writer(listenerPort(*addr), true), NextProtos: []string{protocol.ALPN}, MinVersion: tls.VersionTLS13}
	var ln echo.Listener
	if *addr != *gatewayQUIC {
		separate, listenErr := quic.ListenAddr(*addr, echoTLS, &quic.Config{MaxIdleTimeout: 90 * time.Second, MaxIncomingStreams: 1, MaxIncomingUniStreams: -1})
		if listenErr != nil {
			log.Error("listen", "error", listenErr)
			os.Exit(1)
		}
		defer separate.Close()
		ln = separate
	}
	log.Info("listening", "address", *addr, "certificate_sha256", labcert.Fingerprint(cert))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *fallback != "" && (*publicHTTPS == "" || *tlsHost == "") {
		log.Error("tls-fallback requires https-listen and tls-host")
		os.Exit(1)
	}
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				if *ephemeral {
					continue
				}
				if e := reloadable.Reload(*certPath, *keyPath); e != nil {
					log.Error("certificate_reload_failed", "error", e)
				} else {
					log.Info("certificate_reloaded")
				}
			}
		}
	}()
	var publicAdmin http.Handler
	var adminBase string
	var captureManager *debugcapture.Manager
	var managed *admin.Store
	var uplink *transit.Config
	var echoProbe func(context.Context) error
	if *adminFile != "" {
		cfg, e := admin.ReadConfig(*adminFile)
		if e != nil {
			log.Error("admin_config", "error", e)
			os.Exit(1)
		}
		uplink = cfg.Transit
		if uplink != nil {
			echoProbe = uplink.Probe
		}
		managed, e = admin.OpenStore(cfg.DataDir)
		if e != nil {
			log.Error("admin_store", "error", e)
			os.Exit(1)
		}
		managed.ConfigureDeviceLimit(cfg.DeviceLimit)
		if cfg.VLESS != nil {
			stopVLESSAdmission, err := managed.StartVLESSAdmission(ctx)
			if err != nil {
				log.Error("vless_admission_unavailable")
				os.Exit(1)
			}
			defer stopVLESSAdmission()
			if err := managed.ConfigureVLESS(cfg.VLESS, &vlessserver.ControlClient{Dir: filepath.Join(cfg.DataDir, "vless")}); err != nil {
				log.Warn("vless_application_pending")
			}
			go managed.WatchVLESS(ctx)
		}
		if e = managed.ConfigureAWG(cfg.AWG); e != nil {
			log.Error("awg_config", "error", e)
			os.Exit(1)
		}
		if cfg.AWG != nil {
			stopAdmission, admissionErr := managed.StartAWGAdmission(ctx)
			if admissionErr != nil {
				log.Error("device_admission", "error", admissionErr)
				os.Exit(1)
			}
			defer stopAdmission()
			log.Info("device_admission_recovering", "ready_at", managed.AdmissionReadyAt())
			managed.SetAWGReload(func() error { return awgserver.Reload(cfg.DataDir) })
			go managed.WatchAWG(ctx, cfg.DataDir)
		}
		cfg.Capabilities = opts.Capabilities
		ui := admin.NewWeb(cfg, managed)
		if cfg.Capture != nil {
			portOf := func(addr string) int { _, p, _ := net.SplitHostPort(addr); n, _ := strconv.Atoi(p); return n }
			echoTCP := portOf(cfg.Echo.HTTPS)
			if echoTCP == 0 {
				u, _ := url.Parse(cfg.PublicURL)
				echoTCP = 443
				if u.Port() != "" {
					echoTCP, _ = strconv.Atoi(u.Port())
				}
			}
			cfg.Capture.Ports = map[string][2]int{"echo": {portOf(*addr), echoTCP}, "vpn": {portOf(*gatewayQUIC), portOf(*gatewayHTTPS)}}
			cfg.Capture.Termination = map[string][2]bool{"echo": {true, *publicHTTPS != "" && listenerPort(*publicHTTPS) == echoTCP}, "vpn": {*gatewayQUIC != "", *gatewayHTTPS != ""}}
			ui.Capture, e = debugcapture.New(ctx, *cfg.Capture, nil)
			if e != nil {
				log.Error("capture_config", "error", e)
				os.Exit(1)
			}
			keys.Set(ui.Capture)
			captureManager = ui.Capture
			managed.ConfigureCapture(captureManager)
		}
		if cfg.EchoAWG != "" {
			var probe func(context.Context) error
			if uplink != nil {
				probe = uplink.Probe
			}
			publicAWG, e := echoawg.Start(ctx, cfg.EchoAWG, probe)
			if e != nil {
				log.Error("echo_awg", "error", e)
				os.Exit(1)
			}
			defer publicAWG.Close()
			ui.PublicAWG = publicAWG
		}
		ui.ListenerBindings = map[string]string{"public": *publicHTTPS, "echo-https": *webListen, "echo-quic": *addr, "vpn-quic": *gatewayQUIC, "vpn-https": *gatewayHTTPS}
		publicAdmin = ui.Handler()
		adminBase = cfg.PublicURL
		as := &http.Server{Addr: cfg.Listen, Handler: withCapabilities(ui.Handler(), opts.Capabilities), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
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
		gw.BondContext = ctx
		gw.BondOptions = bond.Options{DisconnectGrace: time.Duration(opts.BondDisconnectGraceSeconds) * time.Second}
		if uplink != nil {
			gw.DialContext = uplink.DialContext
			gw.Probe = uplink.Probe
			gw.Resolver = uplink.Resolver()
		}
		var mtls *tls.Config
		if managed != nil {
			mtls = managed.TLS(cert)
			mtls.NextProtos = []string{gateway.ALPN, gateway.BondALPN}
			gw.RegisterProtocol = managed.RegisterProtocol
			gw.Track = managed.Track
			gw.Multiplexed = managed.TrackMultiplexed
			if captureManager != nil {
				gw.Capture = func(cs tls.ConnectionState, network, address string, c net.Conn) net.Conn {
					device, captureErr := managed.DeviceForTLS(cs)
					if captureErr != nil || managed.CaptureEligible(device.ID) != nil {
						return c
					}
					return captureManager.Wrap(device.UserID, network, address, c)
				}
			}
		} else {
			mtls, e = gateway.TLS(cert, *clientCA)
		}
		if e != nil {
			log.Error("gateway_tls", "error", e)
			os.Exit(1)
		}
		mtls.Certificates = nil
		mtls.GetCertificate = reloadable.GetCertificate
		mtls.KeyLogWriter = keys.Writer(listenerPort(*gatewayQUIC), true)
		if *gatewayQUIC != "" {
			var ql gateway.QUICListener
			qt := servertls.AllowServerNames(mtls, *tlsHost, opts.VPNSNINames)
			qc := &quic.Config{EnableDatagrams: true, MaxIdleTimeout: 90 * time.Second, KeepAlivePeriod: 2 * time.Second, MaxIncomingStreams: gateway.MaxFlows, MaxIncomingUniStreams: -1}
			if *addr == *gatewayQUIC {
				shared, listenErr := newSharedQUICListener(ctx, *addr, echoTLS, qt, qc)
				if listenErr != nil {
					log.Error("shared_quic_listen", "error", listenErr)
					os.Exit(1)
				}
				defer shared.Close()
				ln, ql = shared.Echo, shared.VPN
			} else {
				separate, listenErr := quic.ListenAddr(*gatewayQUIC, qt, qc)
				if listenErr != nil {
					log.Error("gateway_listen", "error", listenErr)
					os.Exit(1)
				}
				defer separate.Close()
				ql = separate
			}
			go func() {
				if e := gw.ServeQUIC(ctx, ql); e != nil && ctx.Err() == nil {
					log.Error("gateway_quic", "error", e)
					cancel()
				}
			}()
		}
		if *gatewayHTTPS != "" {
			tc := mtls.Clone()
			if *gatewayHTTPS != *publicHTTPS {
				tc = servertls.AllowServerNames(tc, *tlsHost, opts.VPNSNINames)
			}
			tc.KeyLogWriter = keys.Writer(listenerPort(*gatewayHTTPS), false)
			tc.NextProtos = []string{"http/1.1"}
			mux := http.NewServeMux()
			mux.HandleFunc("/tunnel", gw.WebSocket)
			mux.HandleFunc("/tunnel/bond", gw.ServeBondHTTPS)
			var handler http.Handler = clientCertificateRequired(vpnTunnelHandler(mux, *tlsHost, opts.VPNSNINames))
			tc = publicTLS(tc)
			if *publicHTTPS == *gatewayHTTPS {
				handler = publicHandler(publicAdmin, adminBase, log, vpnTunnelHandler(mux, *tlsHost, opts.VPNSNINames), echoProbe)
			}
			handler = withCapabilities(handler, opts.Capabilities)
			hs := &http.Server{Addr: *gatewayHTTPS, Handler: handler, TLSConfig: tc, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16384}
			if *publicHTTPS == *gatewayHTTPS {
				configurePublicTLS(hs, opts, log)
			}
			defer hs.Close()
			go func() {
				if e := servePublicTLS(ctx, hs, *publicHTTPS == *gatewayHTTPS, *tlsHost, *fallback, opts.TLSRoutes, opts.VPNSNINames...); e != nil && e != http.ErrServerClosed {
					log.Error("gateway_https", "error", e)
					cancel()
				}
			}()
		}
	}
	if *publicHTTPS != "" && *publicHTTPS != *gatewayHTTPS {
		ps := &http.Server{Addr: *publicHTTPS, Handler: withCapabilities(publicHandler(publicAdmin, adminBase, log, nil, echoProbe), opts.Capabilities), TLSConfig: &tls.Config{GetCertificate: reloadable.GetCertificate, KeyLogWriter: keys.Writer(listenerPort(*publicHTTPS), false), MinVersion: tls.VersionTLS13, NextProtos: []string{"http/1.1"}}, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16384}
		defer ps.Close()
		configurePublicTLS(ps, opts, log)
		go func() {
			if e := servePublicTLS(ctx, ps, true, *tlsHost, *fallback, opts.TLSRoutes, opts.VPNSNINames...); e != nil && e != http.ErrServerClosed {
				log.Error("public_https", "error", e)
				cancel()
			}
		}()
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
		mux.Handle("/echo", echo.WebSocketHandler(log, echoProbe))
		srv := &http.Server{Addr: *webListen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second}
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error("websocket_listen", "error", err)
				cancel()
			}
		}()
		defer srv.Close()
	}
	if err := echo.Serve(ctx, ln, log, echoProbe); err != nil && ctx.Err() == nil {
		log.Error("serve", "error", err)
		os.Exit(1)
	}
}

func listenerPort(addr string) int {
	_, p, _ := net.SplitHostPort(addr)
	n, _ := strconv.Atoi(p)
	return n
}
func servePublicTLS(ctx context.Context, s *http.Server, public bool, host, fallback string, routes []sniRoute, allowedNames ...string) error {
	if !public || (fallback == "" && len(routes) == 0) {
		return s.ListenAndServeTLS("", "")
	}
	ln, e := net.Listen("tcp", s.Addr)
	if e != nil {
		return e
	}
	routed, e := newSNIRouterWithRoutes(ctx, ln, host, fallback, routes, allowedNames...)
	if e != nil {
		ln.Close()
		return fmt.Errorf("TLS frontend: %w", e)
	}
	defer routed.Close()
	return s.ServeTLS(routed, "", "")
}
