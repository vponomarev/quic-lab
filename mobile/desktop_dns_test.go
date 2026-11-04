//go:build (linux && !android) || windows

package mobile

import (
	"context"
	"errors"
	"golang.org/x/net/dns/dnsmessage"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type desktopDNSFake struct {
	mode    string
	calls   atomic.Int32
	started chan struct{}
}

func (f *desktopDNSFake) Close() error { return nil }
func (f *desktopDNSFake) DialContext(ctx context.Context, network, target string) (net.Conn, error) {
	f.calls.Add(1)
	if network != "udp4" || target != "192.0.2.2:53" {
		return nil, errors.New("unexpected probe destination")
	}
	a, b := net.Pipe()
	go func() {
		defer b.Close()
		raw := make([]byte, 4096)
		n, err := b.Read(raw)
		if err != nil {
			return
		}
		var m dnsmessage.Message
		if m.Unpack(raw[:n]) != nil {
			return
		}
		if f.started != nil {
			close(f.started)
		}
		m.Response = true
		m.RCode = dnsmessage.RCodeNameError
		switch f.mode {
		case "id":
			m.ID++
		case "question":
			m.Questions[0].Type = dnsmessage.TypeAAAA
		case "name":
			m.Questions[0].Name, _ = dnsmessage.NewName("stale.invalid.")
		case "case":
			m.Questions[0].Name, _ = dnsmessage.NewName(strings.ToUpper(m.Questions[0].Name.String()))
		case "class":
			m.Questions[0].Class = dnsmessage.ClassCHAOS
		case "missing":
			m.Questions = nil
		case "success":
			m.RCode = dnsmessage.RCodeSuccess
		case "query":
			m.Response = false
		case "opcode":
			m.OpCode = 1
		case "truncated":
			m.Truncated = true
		case "servfail":
			m.RCode = dnsmessage.RCodeServerFailure
		case "timeout":
			b.Read(raw)
			return
		}
		raw, _ = m.Pack()
		b.Write(raw)
	}()
	return a, nil
}
func TestDesktopDNSProbeIntegrityAndNoDirect(t *testing.T) {
	for _, mode := range []string{"good", "success", "case", "id", "question", "name", "class", "missing", "query", "opcode", "truncated", "servfail", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			f := &desktopDNSFake{mode: mode}
			g := NewGateway(nil)
			g.cfg.Transport = "vless"
			g.vless = f
			g.ctx = context.Background()
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			err := g.ProbeDesktopDNS(ctx, "192.0.2.2")
			if (mode == "good" || mode == "success" || mode == "case") != (err == nil) {
				t.Fatal("unexpected integrity result", err)
			}
			if f.calls.Load() != 1 {
				t.Fatal("probe did not use VPN backend")
			}
		})
	}
	f := &desktopDNSFake{mode: "good"}
	g := NewGateway(nil)
	g.direct = f
	g.ctx = context.Background()
	if g.ProbeDesktopDNS(context.Background(), "192.0.2.2") == nil || f.calls.Load() != 0 {
		t.Fatal("direct health probe")
	}
	g.direct = nil
	g.cfg.Transport = "vless"
	g.vless = f
	for _, bad := range []string{"resolver.example", "127.0.0.1", "::1", "192.0.2.2:53"} {
		if g.ProbeDesktopDNS(context.Background(), bad) == nil {
			t.Fatal("invalid resolver")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if g.ProbeDesktopDNS(ctx, "192.0.2.2") == nil || f.calls.Load() != 0 {
		t.Fatal("cancelled probe dialed")
	}
	g.ctx = nil
	if g.ProbeDesktopDNS(context.Background(), "192.0.2.2") == nil || f.calls.Load() != 0 {
		t.Fatal("stopped gateway dialed")
	}
}

func TestDesktopDNSProbeOwnerCancellation(t *testing.T) {
	f := &desktopDNSFake{mode: "timeout", started: make(chan struct{})}
	g := NewGateway(nil)
	g.cfg.Transport = "vless"
	g.vless = f
	owner, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.ctx = owner
	done := make(chan error, 1)
	go func() { done <- g.ProbeDesktopDNS(context.Background(), "192.0.2.2") }()
	select {
	case <-f.started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled probe succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("owner cancellation did not close socket")
	}
}
