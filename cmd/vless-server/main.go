//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
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
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	w, e := vlessserver.OpenWorker(ctx, *dir, func(ctx context.Context, id string) (time.Duration, error) {
		return vlessserver.RequestAdmission(ctx, *admission, id)
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
