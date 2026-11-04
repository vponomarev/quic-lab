//go:build linux && !android

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"quiclab/internal/approuting"
	"quiclab/internal/linuxclient"
	"sync"
	"syscall"
	"time"
)

type sink struct {
	mu     sync.Mutex
	writer io.Writer
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
	}
}
func main() { os.Exit(run()) }
func run() int {
	path := flag.String("config", "", "private JSON profile (0600)")
	validate := flag.Bool("validate", false, "validate without network changes")
	flag.Parse()
	f, e := os.Open(*path)
	if e != nil {
		fmt.Fprintln(os.Stderr, "cannot read configuration")
		return 2
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		fmt.Fprintln(os.Stderr, "configuration must be a regular private file (0600)")
		return 2
	}
	raw, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil {
		return 2
	}
	cfg, e := linuxclient.Decode(raw)
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
		if cfg.AppRouting != nil && cfg.AppRouting.Enabled {
			unknown := cfg.AppRouting.Unknown
			if unknown == "" {
				unknown = approuting.Tunnel
			}
			fmt.Printf("application routing: %s; unknown: %s; capture: %d selected IPv4 subnets; captured DNS: VPN; IPv6/system DNS unchanged; apply requires restart\n", cfg.AppRouting.Mode, unknown, len(cfg.Targets))
		}
		return 0
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "run as root or through the provided service")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	output := &sink{}
	client := &linuxclient.Client{Sink: output}
	defer client.Stop()
	// Gateway dial timeouts are bounded internally; the session follows process lifetime.
	if e = client.Start(ctx, cfg); e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 1
	}
	output.write(map[string]any{"state": "running", "tun": client.Name, "transport": cfg.Transport, "readiness": "engine_ready"})
	output.appRouting(client.AppRoutingSnapshot())
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	running := true
	for running {
		select {
		case <-ctx.Done():
			running = false
		case <-ticker.C:
			output.appRouting(client.AppRoutingSnapshot())
			if !client.Connected() {
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
