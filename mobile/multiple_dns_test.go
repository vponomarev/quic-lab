//go:build linux

package mobile

import (
	"context"
	"errors"
	"io"
	"net"
	"quiclab/internal/trafficbudget"
	"testing"
	"time"
)

func TestTunnelDNSNoFallback(t *testing.T) {
	b := &multipleBinder{}
	m, e := CreateMultiRouterWithDNS(`[{"id":"a","mode":"all"}]`, `{}`, nil, b, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	for _, proto := range []int64{6, 17} {
		if m.selectFlow(proto, "10.0.0.1", 1234, "1.1.1.1", 53) != nil {
			t.Fatal("unavailable DNS escaped")
		}
	}
	if b.calls.Load() != 0 {
		t.Fatal("direct DNS socket opened")
	}
	g := NewGateway(nil)
	if e = m.SetGateway("a", g); e != nil {
		t.Fatal(e)
	}
	for _, proto := range []int64{6, 17} {
		if h := m.selectFlow(proto, "10.0.0.1", 1234, "1.1.1.1", 53); h == nil || h.g != g {
			t.Fatal("DNS did not select configured exit")
		}
	}
	m.RemoveGateway("a")
	for _, proto := range []int64{6, 17} {
		if m.selectFlow(proto, "10.0.0.1", 1234, "1.1.1.1", 53) != nil {
			t.Fatal("failed DNS exit escaped direct")
		}
	}
	if b.calls.Load() != 0 {
		t.Fatal("failed DNS exit opened direct socket")
	}
	if m.selectFlow(6, "10.0.0.1", 1234, "2001:db8::1", 80) != nil {
		t.Fatal("IPv6 escaped")
	}
}
func TestSystemDNSNetworkChange(t *testing.T) {
	m, e := CreateMultiRouterWithDNS(`[{"id":"a","mode":"all"}]`, `{"mode":"system","servers":["192.0.2.53"]}`, nil, &multipleBinder{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	old := m.selectFlow(17, "10.0.0.1", 1234, "1.1.1.1", 53)
	if old == nil {
		t.Fatal("system DNS blocked")
	}
	if e = m.UpdateDNS(`{"mode":"system","servers":[]}`, &multipleBinder{}); e != nil {
		t.Fatal(e)
	}
	if old.ctx.Err() == nil {
		t.Fatal("stale DNS flows retained")
	}
	if m.selectFlow(6, "10.0.0.1", 1234, "1.1.1.1", 53) != nil {
		t.Fatal("missing physical DNS fell back")
	}
	if e = m.UpdateDNS(`{"mode":"system","servers":["192.0.2.54"]}`, &multipleBinder{}); e != nil {
		t.Fatal(e)
	}
	if m.selectFlow(6, "10.0.0.1", 1234, "1.1.1.1", 53) == nil {
		t.Fatal("replacement DNS unavailable")
	}
	m.Close()
	if m.selectFlow(17, "10.0.0.1", 1234, "1.1.1.1", 53) != nil {
		t.Fatal("closed router accepted DNS")
	}
}
func TestSystemDNSRestrictedDestination(t *testing.T) {
	d := protectedDNSDialer{binder: &multipleBinder{}, server: "192.0.2.53"}
	for _, a := range []string{"1.1.1.1:443", "example.com:53", "[::1]:53"} {
		if _, e := d.DialContext(context.Background(), "tcp", a); e == nil {
			t.Fatal("DNS bypass accepted", a)
		}
	}
}

func TestSystemDNSSocketsAndBudget(t *testing.T) {
	budget, e := NewTrafficBudget("dns", 1)
	if e != nil {
		t.Fatal(e)
	}
	b := &multipleBinder{}
	d := protectedDNSDialer{binder: b, server: "127.0.0.53", network: "cell", budget: budget}
	// Consuming the run budget must block resolver dials before creating a socket.
	budget.meter.Receive(1, trafficbudget.User)
	if _, e = d.DialContext(context.Background(), "udp", "1.1.1.1:53"); !errors.Is(e, trafficbudget.ErrBlocked) {
		t.Fatalf("DNS bypassed shared budget: %v", e)
	}
	if b.calls.Load() != 0 {
		t.Fatal("blocked DNS opened socket")
	}
	for _, proto := range []string{"tcp", "udp"} {
		t.Run(proto, func(t *testing.T) {
			var closeServer func()
			if proto == "tcp" {
				ln, e := net.Listen("tcp4", "127.0.0.53:53")
				if e != nil {
					t.Fatal(e)
				}
				closeServer = func() { ln.Close() }
				go func() {
					c, e := ln.Accept()
					if e == nil {
						defer c.Close()
						io.Copy(c, c)
					}
				}()
			} else {
				ln, e := net.ListenPacket("udp4", "127.0.0.53:53")
				if e != nil {
					t.Fatal(e)
				}
				closeServer = func() { ln.Close() }
				go func() {
					buf := make([]byte, 100)
					n, a, e := ln.ReadFrom(buf)
					if e == nil {
						ln.WriteTo(buf[:n], a)
					}
				}()
			}
			defer closeServer()
			run, _ := NewTrafficBudget("socket", 100)
			resolver := protectedDNSDialer{binder: b, server: "127.0.0.53", network: "cell", budget: run}
			c, e := resolver.DialContext(context.Background(), proto, "192.0.2.1:53")
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(time.Second))
			if _, e = c.Write([]byte("dns")); e != nil {
				t.Fatal(e)
			}
			buf := make([]byte, 3)
			if _, e = io.ReadFull(c, buf); e != nil || string(buf) != "dns" {
				t.Fatalf("resolver response %q: %v", buf, e)
			}
			if run.ledger.Snapshot().Used != 6 {
				t.Fatal("DNS socket payload not metered", run.Snapshot())
			}
		})
	}
}

func TestMultipleTrafficIncludesPacketMetrics(t *testing.T) {
	sink := &eventSink{ch: make(chan map[string]any, 20)}
	m, e := NewMultiRouter(`[{"id":"a","mode":"all"}]`, "a", nil, &multipleBinder{}, sink)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	g := NewGateway(nil)
	if e = m.SetGateway("a", g); e != nil {
		t.Fatal(e)
	}
	h := m.profiles["a"].h
	h.tx.Store(31)
	h.rx.Store(47)
	h.tcpFlows.Store(2)
	h.udpFlows.Store(3)
	h.udpTx.Store(7)
	h.udpRx.Store(11)
	h.udpRejected.Store(13)
	m.emitTraffic()
	for {
		select {
		case event := <-sink.ch:
			if event["event"] != "profile_traffic" || event["profile_id"] != "a" {
				continue
			}
			for k, want := range map[string]float64{"tx_bytes": 31, "rx_bytes": 47, "tcp_flows": 2, "udp_flows": 3, "udp_tx": 7, "udp_rx": 11, "udp_rejected": 13, "datagram_drops": 0} {
				if event[k] != want {
					t.Fatalf("metric %s=%v want %v", k, event[k], want)
				}
			}
			return
		case <-time.After(time.Second):
			t.Fatal("no profile traffic event")
		}
	}
}
