//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/tun"
	"quiclab/internal/awgserver"
)

func main() {
	if e := run(); e != nil {
		slog.Error("awg_worker", "error", e)
		os.Exit(1)
	}
}
func run() error {
	path := flag.String("config", "/etc/quic-lab/awg.json", "AWG worker JSON configuration")
	flag.Parse()
	raw, e := os.ReadFile(*path)
	if e != nil {
		return e
	}
	var cfg struct {
		DataDir string           `json:"data_dir"`
		AWG     awgserver.Config `json:"awg"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return errors.New("invalid worker config")
	}
	if e = cfg.AWG.Validate(); e != nil {
		return e
	}
	if cfg.DataDir == "" {
		return errors.New("data_dir required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if _, err := net.InterfaceByName(cfg.AWG.Interface); err == nil {
		return errors.New("AWG interface already exists; refusing to attach")
	}
	t, e := tun.CreateTUN(cfg.AWG.Interface, cfg.AWG.MTU)
	if e != nil {
		return e
	}
	d := device.NewDevice(t, conn.NewStdNetBind(), &device.Logger{Verbosef: func(string, ...any) {}, Errorf: func(string, ...any) {}})
	defer d.Close()
	w := &awgserver.Worker{Device: d, Config: cfg.AWG, Dir: cfg.DataDir}
	if e = w.Tick(); e != nil {
		return e
	}
	if e = exec.CommandContext(ctx, "ip", "-4", "address", "replace", cfg.AWG.Address, "dev", cfg.AWG.Interface).Run(); e != nil {
		return errors.New("cannot assign AWG interface address")
	}
	if e = exec.CommandContext(ctx, "ip", "link", "set", "dev", cfg.AWG.Interface, "up").Run(); e != nil {
		return errors.New("cannot bring AWG interface up")
	}
	if e = d.Up(); e != nil {
		return e
	}
	slog.Info("AWG ready", "interface", cfg.AWG.Interface, "udp_port", cfg.AWG.Port())
	return w.Run(ctx)
}
