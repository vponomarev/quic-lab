package main

import (
	"context"
	"flag"
	"fmt"
	"golang.org/x/sys/windows/svc"
	"os"
	"os/signal"
	"path/filepath"
	"quiclab/internal/vpnclient"
	"quiclab/internal/windowsclient"
	"time"
)

func main() {
	owner := flag.String("owner-sid", "", "allowed owner SID")
	data := flag.String("data-dir", "", "private absolute state directory")
	fixture := flag.Bool("fixture", false, "foreground fixture; no VPN")
	pipe := flag.String("pipe", windowsclient.ServicePipe, "fixture pipe name")
	engine := flag.Bool("engine", false, "use administrator-provisioned engine.json from private data directory")
	flag.Parse()
	if *engine && *fixture {
		fmt.Fprintln(os.Stderr, "real engine requires SCM mode")
		os.Exit(2)
	}
	if *owner == "" || !filepath.IsAbs(*data) {
		fmt.Fprintln(os.Stderr, "owner SID and absolute data directory required")
		os.Exit(2)
	}
	if !*fixture {
		isService, e := svc.IsWindowsService()
		if e != nil || !isService {
			fmt.Fprintln(os.Stderr, "SCM required; use -fixture only for explicit no-network tests")
			os.Exit(2)
		}
		if svc.Run(windowsclient.ServiceName, &windowsclient.Service{OwnerSID: *owner, DataDir: *data, Engine: *engine}) != nil {
			os.Exit(1)
		}
		return
	}
	store, e := windowsclient.NewEncryptedStore(*data)
	if e != nil {
		fmt.Fprintln(os.Stderr, "store unavailable")
		os.Exit(1)
	}
	l, e := windowsclient.Listen(*pipe, *owner)
	if e != nil {
		fmt.Fprintln(os.Stderr, "pipe unavailable")
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	controller := vpnclient.NewController(windowsclient.FixtureBackend{})
	fmt.Println("fixture_ready")
	windowsclient.Serve(ctx, l, *owner, windowsclient.NewHandler(controller, store))
	cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	controller.Stop(cleanup)
}
