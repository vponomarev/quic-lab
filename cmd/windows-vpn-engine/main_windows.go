//go:build windows

// windows-vpn-engine is a bounded, foreground Windows MVP. It never installs a service.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/miekg/dns"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"quiclab/internal/vpnclient"
	"quiclab/internal/windowsengine"
	"strings"
	"time"
)

type sink struct{}

func (sink) OnEvent(raw string) {
	var v map[string]any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return
	}
	switch v["event"] {
	case "connected", "disconnected", "traffic":
		delete(v, "detail")
		b, _ := json.Marshal(v)
		fmt.Println(string(b))
	}
}
func main() { os.Exit(run()) }
func run() int {
	file := flag.String("config", "", "private JSON settings")
	duration := flag.Duration("duration", 2*time.Minute, "bounded foreground lifetime")
	probe := flag.Bool("probe", false, "verify HTTPS and DNS through 1.1.1.1")
	expected := flag.String("expected-exit", "", "required IPv4 server exit for probe")
	validate := flag.Bool("validate", false, "validate configuration only; no network changes")
	flag.Parse()
	if *duration < time.Second || *duration > 30*time.Minute {
		return 2
	}
	f, e := os.Open(*file)
	if e != nil {
		fmt.Println("configuration unavailable")
		return 2
	}
	raw, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	f.Close()
	if e != nil || len(raw) > 1<<20 {
		return 2
	}
	s, e := windowsengine.ParseSettings(raw)
	if e != nil {
		fmt.Println("invalid configuration")
		return 2
	}
	if *validate {
		fmt.Println("configuration_valid")
		return 0
	}

	if *probe {
		if e = s.ValidateProbe(*expected); e != nil {
			fmt.Println("invalid probe scope")
			return 2
		}
	}
	engine := windowsengine.New(s, windowsengine.WindowsPlatform{}, &windowsengine.NativeGateway{Sink: sink{}})
	c := vpnclient.NewController(engine)
	root, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	ctx, end := context.WithTimeout(root, *duration)
	defer end()
	if e = c.Start(ctx, engine.Config()); e != nil {
		fmt.Println("start_failed:", e)
		c.Stop(context.Background())
		return 1
	}
	fmt.Println("engine_running", s.Transport)
	result := 0
	if *probe {
		if e = check(ctx, *expected); e != nil {
			fmt.Println("probe_failed:", e)
			result = 1
		}
	} else {
		<-ctx.Done()
	}
	clean, done := context.WithTimeout(context.Background(), 20*time.Second)
	defer done()
	if e = c.Stop(clean); e != nil {
		fmt.Println("cleanup_failed")
		return 1
	}
	fmt.Println("engine_stopped")
	return result
}
func check(ctx context.Context, expected string) error {
	// Standard TLS validation, no proxy environment and no verification bypass.
	tr := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext}
	defer tr.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://1.1.1.1/cdn-cgi/trace", nil)
	r, e := (&http.Client{Transport: tr, Timeout: 15 * time.Second}).Do(req)
	if e != nil {
		return e
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 8192))
	r.Body.Close()
	if e != nil || r.StatusCode != 200 {
		return fmt.Errorf("HTTPS probe failed")
	}
	if e = windowsengine.VerifyProbeExit(string(b), expected); e != nil {
		return e
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "ip=") {
			if strings.TrimPrefix(line, "ip=") != expected {
				return fmt.Errorf("unexpected exit IPv4")
			}
			fmt.Println("https_exit", line)
		}
	}
	for _, network := range []string{"udp", "tcp"} {
		m := new(dns.Msg)
		m.SetQuestion("example.org.", dns.TypeA)
		reply, _, e := (&dns.Client{Net: network, Timeout: 8 * time.Second}).ExchangeContext(ctx, m, "1.1.1.1:53")
		if e != nil {
			return e
		}
		if reply.Rcode != dns.RcodeSuccess || len(reply.Answer) == 0 {
			return fmt.Errorf("DNS empty")
		}
		fmt.Println("dns_" + network + "_pass")
	}
	return nil
}
