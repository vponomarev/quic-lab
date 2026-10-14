package windowsengine

import (
	"context"
	"github.com/amnezia-vpn/amneziawg-go/conn"
	"net/netip"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestWindowsStandaloneStartup(t *testing.T) {
	path := os.Getenv("QUIC_LAB_WINDOWS_TRANSPORT_TEST")
	if path == "" {
		t.Skip("explicit private fixture required")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	s, e := ParseSettings(raw)
	if e != nil {
		t.Fatal("parse", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	g := &NativeGateway{}
	defer g.Stop()
	if e = g.Start(ctx, s); e != nil {
		t.Fatal("gateway start", e)
	}
}

func TestWindowsAWGSocketBinding(t *testing.T) {
	path := os.Getenv("QUIC_LAB_WINDOWS_TRANSPORT_TEST")
	if path == "" {
		t.Skip("opt in")
	}
	raw, _ := os.ReadFile(path)
	s, e := ParseSettings(raw)
	if e != nil {
		t.Fatal(e)
	}
	b := conn.NewDefaultBind()
	_, _, e = b.Open(0)
	if e != nil {
		t.Fatal("open", e)
	}
	defer b.Close()
	i := b.(conn.BindSocketToInterface)
	if e = i.BindSocketToInterface4(s.InterfaceIndex, false); e != nil {
		t.Fatal("v4", e)
	}
	if e = i.BindSocketToInterface6(0, true); e != nil {
		t.Fatal("disable unused IPv6", e)
	}
}

func TestWindowsBoundHostnameResolution(t *testing.T) {
	v := os.Getenv("QUIC_LAB_WINDOWS_RESOLVE_INTERFACE")
	if v == "" {
		t.Skip("opt-in physical interface")
	}
	id, e := strconv.ParseUint(v, 10, 32)
	if e != nil {
		t.Fatal(e)
	}
	s := valid()
	s.Endpoint = "quic-lab.vpnc.ru:443"
	s.InterfaceIndex = uint32(id)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resolved, e := (WindowsPlatform{}).Resolve(ctx, s)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = netip.ParseAddrPort(resolved.Endpoint); e != nil {
		t.Fatal("not numeric", e)
	}
	if s.Endpoint != "quic-lab.vpnc.ru:443" {
		t.Fatal("saved hostname mutated")
	}
	stopped, stop := context.WithCancel(context.Background())
	stop()
	if _, e = (WindowsPlatform{}).Resolve(stopped, s); e == nil {
		t.Fatal("cancellation ignored")
	}
}
