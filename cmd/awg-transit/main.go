//go:build linux

package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/tun"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"quiclab/internal/awg"
	"quiclab/internal/transit"
	"syscall"
	"time"
)

func main() {
	if e := run(); e != nil {
		slog.Error("AWG transit", "error", e)
		os.Exit(1)
	}
}
func run() error {
	inspect := flag.String("inspect-awg", "", "Validate AWG file and print public metadata only")
	path := flag.String("config", "/etc/quic-lab/transit.json", "Private transit configuration")
	flag.Parse()
	if *inspect != "" {
		raw, e := os.ReadFile(*inspect)
		if e != nil {
			return e
		}
		c, e := awg.Parse(string(raw))
		if e != nil {
			return e
		}
		_, e = os.Stdout.WriteString(c.Metadata() + "\n")
		return e
	}
	raw, e := os.ReadFile(*path)
	if e != nil {
		return e
	}
	var cfg struct {
		Transit transit.Config `json:"transit"`
		AWG     string         `json:"awg_config"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return errors.New("invalid transit config")
	}
	if e = cfg.Transit.Validate(); e != nil {
		return e
	}
	parsed, e := awg.Parse(cfg.AWG)
	if e != nil {
		return e
	}
	if parsed.Address != cfg.Transit.SourceIP {
		return errors.New("transit source mismatch")
	}
	full := false
	for _, p := range parsed.Allowed {
		full = full || p == "0.0.0.0/0"
	}
	if !full {
		return errors.New("transit requires IPv4 default AllowedIPs")
	}
	if _, e = net.InterfaceByName("ql-exit0"); e == nil {
		return errors.New("refusing existing ql-exit0")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	t, e := tun.CreateTUN("ql-exit0", parsed.MTU)
	if e != nil {
		return e
	}
	d := device.NewDevice(t, conn.NewStdNetBind(), &device.Logger{Verbosef: func(string, ...any) {}, Errorf: func(string, ...any) {}})
	defer d.Close()
	if e = d.IpcSet(parsed.IPC(cfg.Transit.Endpoint) + "persistent_keepalive_interval=25\n"); e != nil {
		return errors.New("transit AWG configuration rejected")
	}
	for _, args := range [][]string{{"address", "add", parsed.Address + "/32", "dev", "ql-exit0"}, {"link", "set", "dev", "ql-exit0", "up"}, {"route", "replace", "table", "51821", "default", "dev", "ql-exit0", "metric", "10"}} {
		if e = exec.CommandContext(ctx, "ip", append([]string{"-4"}, args...)...).Run(); e != nil {
			return errors.New("transit interface/route setup failed")
		}
	}
	if e = d.Up(); e != nil {
		return e
	}
	socket := cfg.Transit.ProbeSocket
	if filepath.Base(socket) != "transit.sock" {
		return errors.New("unexpected probe socket name")
	}
	if st, e := os.Lstat(socket); e == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return errors.New("probe path is not a socket")
		}
		if e = os.Remove(socket); e != nil {
			return e
		}
	}
	listener, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	defer listener.Close()
	defer os.Remove(socket)
	if e = os.Chmod(socket, 0600); e != nil {
		return e
	}
	target := netip.MustParseAddrPort(cfg.Transit.Endpoint).Addr().String()
	slots := make(chan struct{}, 32)
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/probe" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "busy", 503)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
		defer cancel()
		if e := probe(ctx, cfg.Transit.SourceIP, target); e != nil {
			http.Error(w, "gateway unavailable", 503)
			return
		}
		w.WriteHeader(204)
	})}
	defer server.Close()
	go func() { <-ctx.Done(); server.Close() }()
	slog.Info("AWG transit ready", "gateway", target)
	e = server.Serve(listener)
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}
func probe(ctx context.Context, source, target string) error {
	socket, e := icmp.ListenPacket("ip4:icmp", source)
	if e != nil {
		return e
	}
	defer socket.Close()
	deadline, ok := ctx.Deadline()
	if ok {
		socket.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { socket.Close() })
	defer stop()
	var random [18]byte
	if _, e = rand.Read(random[:]); e != nil {
		return e
	}
	id := int(binary.BigEndian.Uint16(random[:2]))
	data := string(random[2:])
	request := icmp.Message{Type: ipv4.ICMPTypeEcho, Body: &icmp.Echo{ID: id, Seq: 1, Data: []byte(data)}}
	packet, e := request.Marshal(nil)
	if e != nil {
		return e
	}
	if _, e = socket.WriteTo(packet, &net.IPAddr{IP: net.ParseIP(target)}); e != nil {
		return e
	}
	buffer := make([]byte, 2048)
	for {
		n, peer, e := socket.ReadFrom(buffer)
		if e != nil {
			return e
		}
		if peer.String() != target {
			continue
		}
		reply, e := icmp.ParseMessage(1, buffer[:n])
		if e != nil || reply.Type != ipv4.ICMPTypeEchoReply {
			continue
		}
		body, ok := reply.Body.(*icmp.Echo)
		if ok && body.ID == id && body.Seq == 1 && string(body.Data) == data {
			return nil
		}
	}
}
