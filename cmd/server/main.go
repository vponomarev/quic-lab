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
	"quiclab/internal/echo"
	"quiclab/internal/labcert"
	"quiclab/internal/protocol"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:4433", "UDP listen address")
	certPath := flag.String("cert", "", "PEM certificate file")
	keyPath := flag.String("key", "", "PEM private key file")
	ephemeral := flag.Bool("ephemeral-cert", false, "generate an in-memory lab certificate; pin printed SHA-256 on client")
	webListen := flag.String("web-listen", "", "optional loopback HTTP WebSocket endpoint behind nginx")
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
