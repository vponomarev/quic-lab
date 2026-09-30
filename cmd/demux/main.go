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
	"os"
	"os/signal"
	"quiclab/internal/gateway"
	"syscall"
	"time"
)

type config struct {
	Listen   string `json:"listen"`
	Cert     string `json:"cert"`
	Key      string `json:"key"`
	ClientCA string `json:"client_ca"`
	Allow    string `json:"allow"`
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
	var cfg config
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if e = d.Decode(&cfg); e != nil {
		return e
	}
	if cfg.Listen == "" || cfg.Cert == "" || cfg.Key == "" || cfg.ClientCA == "" || cfg.Allow == "" {
		return errors.New("listen, cert, key, client_ca and allow are required")
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
