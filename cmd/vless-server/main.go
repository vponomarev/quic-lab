//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"quiclab/internal/backup"
	"quiclab/internal/vless"
	"quiclab/internal/vlessserver"
	"syscall"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "VLESS worker failed")
		os.Exit(1)
	}
}
func run() error {
	dir := flag.String("data-dir", "/var/lib/quic-lab/vless", "private VLESS worker directory")
	admission := flag.String("admission-dir", "/var/lib/quic-lab", "private admission broker directory")
	check := flag.String("check-config", "", "validate private typed configuration without starting services")
	health := flag.Bool("check-applied", false, "check worker applied revision without modifying state")
	revision := flag.Uint64("revision", 0, "expected revision for health check")
	flag.Parse()
	if *health {
		return (vlessserver.ControlClient{Dir: *dir}).CheckApplied(context.Background(), *revision)
	}
	if *check != "" {
		_, e := vlessserver.ReadConfig(*check)
		return e
	}
	installation, e := backup.AcquireInstallationLock(*admission, false)
	if e != nil {
		return e
	}
	defer installation.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	w, e := vlessserver.OpenWorker(ctx, *dir, func(ctx context.Context, id, uuid string) (time.Duration, error) {
		return vlessserver.RequestAdmission(ctx, *admission, id, uuid, vless.AuthenticatedPeer(ctx))
	})
	if e != nil {
		return e
	}
	defer w.Close()
	stop, e := vlessserver.ServeControl(ctx, w)
	if e != nil {
		return e
	}
	defer stop()
	<-ctx.Done()
	return nil
}
