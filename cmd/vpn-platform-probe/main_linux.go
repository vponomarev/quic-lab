//go:build linux

// vpn-platform-probe runs only in an explicitly isolated Linux network namespace.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"quiclab/internal/vpnclient"
	"quiclab/internal/vpnclienttest"
	"quiclab/internal/vpnmodel"
	"syscall"
)

func main() {
	path := flag.String("config", "", "private gateway JSON file")
	name := flag.String("tun", "qlprobe", "probe TUN name")
	flag.Parse()
	here, _ := os.Readlink("/proc/self/ns/net")
	host, _ := os.Readlink("/proc/1/ns/net")
	if here == "" || here == host {
		fmt.Fprintln(os.Stderr, "isolated network namespace required")
		os.Exit(2)
	}
	raw, e := os.ReadFile(*path)
	if e != nil || len(raw) > 1<<20 {
		fmt.Fprintln(os.Stderr, "configuration unavailable")
		os.Exit(2)
	}
	var wire struct {
		Transport string `json:"transport"`
		Endpoint  string `json:"endpoint"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		os.Exit(2)
	}
	cfg := vpnclient.Config{Revision: 1, Profiles: vpnmodel.Config{Version: 1, Exits: []vpnmodel.Exit{{ID: "probe", Name: "probe", Kind: "standalone"}}, Profiles: []vpnmodel.Profile{{ID: "probe", ExitID: "probe", Transport: wire.Transport, Endpoint: wire.Endpoint, Mode: "auto", PoolSize: 1}}}}
	c := vpnclient.NewController(vpnclienttest.New(*name, string(raw)))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err := c.Start(ctx, cfg)
	json.NewEncoder(os.Stdout).Encode(c.Snapshot())
	if err == nil {
		<-ctx.Done()
	}
	stopErr := c.Stop(context.Background())
	json.NewEncoder(os.Stdout).Encode(c.Snapshot())
	if err != nil || stopErr != nil {
		os.Exit(1)
	}
}
