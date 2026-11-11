//go:build linux && !android

package linuxcli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"quiclab/internal/approuting"
	"quiclab/internal/exitcontrol"
	"quiclab/internal/linuxclient"
	"sync"
	"syscall"
	"time"
)

type sink struct {
	mu        sync.Mutex
	writer    io.Writer
	transport string
}

func (s *sink) write(v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.writer
	if w == nil {
		w = os.Stdout
	}
	json.NewEncoder(w).Encode(v)
}
func (s *sink) appRouting(stats *approuting.Statistics) {
	if stats != nil {
		s.write(map[string]any{"event": "app_routing", "counters": stats})
	}
}
func (s *sink) OnEvent(raw string) {
	var v map[string]any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return
	}
	if v["event"] == "traffic" {
		s.write(map[string]any{"event": "traffic", "tx_bytes": v["tx_bytes"], "rx_bytes": v["rx_bytes"]})
	} else {
		s.diagnostic(v)
	}
}

// Run is shared by the standalone CLI and the single-file desktop executable.
func Run(args []string) int {
	flags := flag.NewFlagSet("quic-lab-client", flag.ContinueOnError)
	path := flags.String("config", "", "private JSON profile (0600)")
	validate := flags.Bool("validate", false, "validate without network changes")
	listExits := flags.Bool("server-exits", false, "list administrator-permitted server exits; do not start VPN")
	exitID := flags.String("server-exit", "", "select a server exit ID, or default to inherit; do not start VPN")
	exitRevision := flags.String("exit-revision", "", "expected server exit revision (otherwise fetch current revision)")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
		return 2
	}
	cfg, e := readConfig(*path)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	if *validate {
		if _, e = cfg.PrepareApplications(); e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		fmt.Println("configuration valid (server reachability and authentication not tested)")
		if cfg.FullIPv4() {
			fmt.Println("capture: full IPv4 via two /1 routes; existing specific routes preserved; IPv6/system DNS unchanged")
		}
		if cfg.AppRouting != nil && cfg.AppRouting.Enabled {
			unknown := cfg.AppRouting.Unknown
			if unknown == "" {
				unknown = approuting.Tunnel
			}
			capture := fmt.Sprintf("%d selected IPv4 subnets", len(cfg.Targets))
			if cfg.FullIPv4() {
				capture = "full IPv4 with existing route exceptions"
			}
			fmt.Printf("application routing: %s; unknown: %s; capture: %s; captured DNS: VPN; IPv6/system DNS unchanged; app-only reload: SIGHUP\n", cfg.AppRouting.Mode, unknown, capture)
		}
		return 0
	}
	if *listExits || *exitID != "" {
		if (*listExits && *exitID != "") || (*listExits && *exitRevision != "") {
			fmt.Fprintln(os.Stderr, "choose one server exit operation")
			return 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := serverExitCommand(ctx, cfg, *exitID, *exitRevision, os.Stdout, exitcontrol.Client{}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if *exitRevision != "" {
		fmt.Fprintln(os.Stderr, "exit-revision requires server-exit")
		return 2
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "run as root or through the provided service")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP)
	defer signal.Stop(reload)
	output := &sink{transport: cfg.Transport}
	client := &linuxclient.Client{Sink: output}
	defer client.Stop()
	// Gateway dial timeouts are bounded internally; the session follows process lifetime.
	if e = client.Start(ctx, cfg); e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 1
	}
	lastHealth := client.Readiness()
	output.write(map[string]any{"state": "running", "tun": client.Name, "transport": cfg.Transport, "readiness": lastHealth.State})
	output.appRouting(client.AppRoutingSnapshot())
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	running := true
	recovered := make(chan error, 1)
	recovering := false
	backoff := 3 * time.Second
	var nextRetry time.Time
	for running {
		select {
		case <-ctx.Done():
			running = false
		case <-reload:
			next, err := readConfig(*path)
			if err == nil {
				err = client.ReloadApplications(next)
			}
			if err != nil {
				output.write(map[string]string{"event": "reload", "status": "rejected", "reason": "invalid configuration or non-application settings changed"})
			} else {
				output.write(map[string]string{"event": "reload", "status": "applied"})
			}
		case err := <-recovered:
			recovering = false
			state := "ready"
			if err != nil {
				state = "reconnecting"
				nextRetry = time.Now().Add(backoff)
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			} else {
				backoff = 3 * time.Second
				nextRetry = time.Time{}
			}
			output.write(map[string]string{"event": "network", "state": state})
		case <-ticker.C:
			output.appRouting(client.AppRoutingSnapshot())
			health := client.Readiness()
			if health != lastHealth {
				output.write(map[string]any{"event": "readiness", "state": health.State, "consecutive_failures": health.Failures})
				lastHealth = health
			}
			if recovering {
				continue
			}
			if !client.Connected() {
				if client.RecoveryEnabled() {
					if !time.Now().Before(nextRetry) {
						recovering = true
						output.write(map[string]string{"event": "network", "state": "reconnecting"})
						go func() { recovered <- client.RecoverVPN(ctx) }()
					}
					continue
				}
				client.Stop()
				output.appRouting(client.AppRoutingSnapshot())
				output.write(map[string]string{"state": "stopped", "reason": "transport disconnected"})
				return 1
			}
		}
	}
	client.Stop()
	output.appRouting(client.AppRoutingSnapshot())
	output.write(map[string]string{"state": "stopped"})
	return 0
}
