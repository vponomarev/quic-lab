//go:build linux && !android

package mobile

import (
	"context"
	"io"
	"net"
	"net/netip"
	"quiclab/internal/approuting"
	"sync"
	"testing"
	"time"
)

func TestDesktopLivePolicyAndVPNKeepDirectFlow(t *testing.T) {
	g := multipleTestGateway(t, "quic")
	b := &multipleBinder{}
	r := desktopRouter(t, g, &desktopOwner{}, b)
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	client, in := net.Pipe()
	defer client.Close()
	r.HandleTCP(desktopTCP{in, desktopID(1002, ln.Addr())})
	exchange := func() {
		t.Helper()
		client.SetDeadline(time.Now().Add(time.Second))
		if _, e := client.Write([]byte("keep")); e != nil {
			t.Fatal(e)
		}
		v := make([]byte, 4)
		if _, e := io.ReadFull(client, v); e != nil || string(v) != "keep" {
			t.Fatal(string(v), e)
		}
	}
	exchange()
	vpnClient, vpnIn := net.Pipe()
	defer vpnClient.Close()
	r.HandleTCP(desktopTCP{vpnIn, desktopID(1001, ln.Addr())})
	vpnClient.SetDeadline(time.Now().Add(time.Second))
	if _, err := vpnClient.Write([]byte("vpn")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(vpnClient, make([]byte, 3)); err != nil {
		t.Fatal(err)
	}
	p, _ := approuting.CompileTUN(approuting.TUNPolicy{Version: 2, Enabled: true, Mode: approuting.OnlySelected, Only: []string{`C:\app\other.exe`}, Unknown: approuting.Block})
	if e := r.UpdatePolicy(p); e != nil {
		t.Fatal(e)
	}
	f := approuting.FlowKey{Protocol: 6, Source: netip.MustParseAddrPort("10.0.0.2:1002"), Destination: netip.MustParseAddrPort(ln.Addr().String())}
	if r.selectFlow(f) != r.vpn.h {
		t.Fatal("new flow did not see new policy")
	}
	exchange() // Old direct stream remains direct despite now matching VPN.
	if e := r.ReplaceVPN(nil); e != nil {
		t.Fatal(e)
	}
	if _, err := vpnClient.Read(make([]byte, 1)); err == nil {
		t.Fatal("old VPN flow survived retirement")
	}
	tx, rx, _, _ := r.vpnTotals()
	if tx != 3 || rx != 3 {
		t.Fatal("retired VPN counters lost", tx, rx)
	}
	if r.selectFlow(f) != nil {
		t.Fatal("unavailable VPN accepted flow")
	}
	f.Destination = netip.MustParseAddrPort("1.1.1.1:53")
	if r.selectFlow(f) != nil {
		t.Fatal("DNS escaped unavailable VPN")
	}
	exchange()
	if e := r.ReplaceVPN(g); e != nil {
		t.Fatal(e)
	}
	tx, rx, _, _ = r.vpnTotals()
	if tx != 3 || rx != 3 {
		t.Fatal("replacement reset VPN counters", tx, rx)
	}
	exchange()
	if b.calls.Load() != 1 {
		t.Fatal("existing direct redialed", b.calls.Load())
	}
	r.Close()
	if r.UpdatePolicy(p) == nil || r.ReplaceVPN(g) == nil {
		t.Fatal("closed router accepted update")
	}
}

func TestDesktopLiveConcurrentUpdateClose(t *testing.T) {
	r := desktopRouter(t, NewGateway(nil), &desktopOwner{}, &multipleBinder{})
	p, _ := approuting.CompileTUN(approuting.TUNPolicy{Version: 2, Mode: approuting.OnlySelected})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				r.UpdatePolicy(p)
				r.ReplaceVPN(nil)
				r.Snapshot()
				r.emitTraffic()
				r.selectFlow(approuting.FlowKey{Protocol: 6, Source: netip.MustParseAddrPort("10.0.0.2:1001"), Destination: netip.MustParseAddrPort("1.1.1.1:443")})
			}
		}()
	}
	r.Close()
	wg.Wait()
	if r.ctx.Err() != context.Canceled {
		t.Fatal("not canceled")
	}
}
