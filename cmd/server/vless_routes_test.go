package main

import (
	"context"
	"quiclab/internal/vlessserver"
	"testing"
)

func TestManagedVLESSRouteOverlay(t *testing.T) {
	initial := vlessserver.Config{Listen: "127.0.0.1:9444", Security: "tls", ServerName: "vless.example", AcceptProxyProtocol: true}
	legacy := sniRoute{ServerNames: []string{"vless.example"}, Target: initial.Listen, ProxyProtocol: true}
	manual := sniRoute{ServerNames: []string{"manual.example"}, Target: "127.0.0.1:9445"}
	c, err := newManagedRouteController("web.example", []string{"vpn.example"}, []sniRoute{legacy, manual}, initial, []string{"0.0.0.0:443"})
	if err != nil {
		t.Fatal(err)
	}
	next := initial
	next.Security = "reality"
	next.ServerName = "one.example"
	next.RealityServerNames = []string{"one.example", "two.example"}
	if err = c.Apply(context.Background(), next, 2); err != nil {
		t.Fatal(err)
	}
	routes := c.snapshot()
	if len(routes) != 3 || routes["two.example"].Target != initial.Listen || routes["manual.example"].Target != manual.Target {
		t.Fatal("routes lost")
	}
	if _, ok := routes["vless.example"]; ok {
		t.Fatal("old managed SNI retained")
	}
	for _, name := range []string{"WEB.example", "vpn.example", "manual.example"} {
		bad := next
		bad.RealityServerNames = []string{name}
		if c.Validate(bad) == nil {
			t.Fatalf("collision accepted: %s", name)
		}
	}
	if err = c.Apply(context.Background(), initial, 1); err == nil {
		t.Fatal("stale revision accepted")
	}
	if len(c.snapshot()) != 3 {
		t.Fatal("failed update changed routes")
	}
	ambiguous := legacy
	ambiguous.ServerNames = []string{"different.example"}
	if _, err = newManagedRouteController("web.example", nil, []sniRoute{ambiguous}, initial, nil); err == nil {
		t.Fatal("ambiguous ownership accepted")
	}
	bad := initial
	bad.Listen = "127.0.0.1:443"
	if c.Validate(bad) == nil {
		t.Fatal("frontend routing loop accepted")
	}
}
