//go:build linux && !android

// quic-lab is the Linux desktop, engine and privileged controller executable.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"quiclab/internal/desktopcontrol"
	"quiclab/internal/desktopinstall"
	"quiclab/internal/desktopui"
	"quiclab/internal/linuxcli"
	"quiclab/internal/linuxnetwork"
)

func invocation(name string, args []string) (string, []string) {
	if filepath.Base(name) == "quic-lab-network" {
		return "network", args
	}
	if filepath.Base(name) == "quic-lab-client" {
		return "engine", args
	}
	if len(args) == 0 {
		return "gui", args
	}
	if strings.HasPrefix(args[0], "-") && args[0] != "-h" && args[0] != "--help" {
		return "engine", args
	}
	return args[0], args[1:]
}

func main() { os.Exit(run(os.Args[0], os.Args[1:])) }
func run(name string, args []string) int {
	mode, args := invocation(name, args)
	if mode == "engine" {
		return linuxcli.Run(args)
	}
	if mode == "-h" || mode == "--help" || mode == "help" {
		fmt.Println("QUIC Lab\n  quic-lab [gui]             Open desktop window (without sudo)\n  quic-lab status            Print sanitized VPN status\n  quic-lab engine [flags]    Run VPN engine\n  quic-lab control           Run root local controller (systemd)\n  sudo quic-lab install --user LOGIN  Migrate existing Linux installation")
		return 0
	}
	if mode != "install" && mode != "network" && len(args) != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		return 2
	}
	switch mode {
	case "network":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "usage: quic-lab network start|stop")
			return 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := linuxnetwork.Run(ctx, args[0]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	case "gui":
		if err := desktopui.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	case "status":
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		s, err := desktopcontrol.Call(ctx, desktopcontrol.Command{Op: "status"})
		if err != nil {
			fmt.Fprintln(os.Stderr, desktopcontrol.ErrorText(err))
			return 1
		}
		if err = json.NewEncoder(os.Stdout).Encode(s); err != nil {
			return 1
		}
	case "control":
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer cancel()
		c, err := desktopcontrol.New("/etc/quic-lab")
		if err == nil {
			err = desktopcontrol.Serve(ctx, desktopcontrol.DefaultSocket, c)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, desktopcontrol.ErrorText(err))
			return 1
		}
	case "install":
		flags := flag.NewFlagSet("install", flag.ContinueOnError)
		login := flags.String("user", "", "desktop user permitted to control VPN")
		if err := flags.Parse(args); err != nil || *login == "" || flags.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "usage: sudo quic-lab install --user LOGIN")
			return 2
		}
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer cancel()
		backup, err := desktopinstall.Install(ctx, *login)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("Installed QUIC Lab. Backup:", backup)
		fmt.Println("Open QUIC Lab from the application menu, without sudo.")
	default:
		fmt.Fprintln(os.Stderr, "unknown mode; use quic-lab --help")
		return 2
	}
	return 0
}
