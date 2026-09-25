package mobile

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"
)

// Explicit authorized deployment test. All credentials and endpoints come from a
// private profile outside the repository. Down mode verifies both data and RTT.
func TestTransitLive(t *testing.T) {
	path := os.Getenv("QUIC_LAB_TRANSIT_PROFILE")
	if path == "" {
		t.Skip("set QUIC_LAB_TRANSIT_PROFILE")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var p map[string]json.RawMessage
	if e = json.Unmarshal(raw, &p); e != nil {
		t.Fatal(e)
	}
	value := func(key string) string { var v string; json.Unmarshal(p[key], &v); return v }
	down := os.Getenv("QUIC_LAB_TRANSIT_DOWN") == "1"
	probeDown := os.Getenv("QUIC_LAB_TRANSIT_PROBE_DOWN") == "1"
	expected := os.Getenv("QUIC_LAB_TRANSIT_EXIT_IP")
	for _, transport := range []string{"quic", "https", "awg"} {
		t.Run(transport, func(t *testing.T) {
			endpoint := value(transport)
			if transport == "awg" {
				meta, e := ValidateAWGConfig(value("awg_config"))
				if e != nil {
					t.Fatal(e)
				}
				var m struct{ Endpoint string }
				json.Unmarshal([]byte(meta), &m)
				endpoint = m.Endpoint
			}
			host, port, _ := net.SplitHostPort(endpoint)
			ips, e := net.LookupIP(host)
			if e != nil {
				t.Fatal(e)
			}
			numeric := ""
			for _, ip := range ips {
				if ip.To4() != nil {
					numeric = net.JoinHostPort(ip.String(), port)
					break
				}
			}
			cfg := gatewayConfig{TransitEndpoint: value("transit_endpoint"), Transport: transport, Endpoint: numeric, Hostname: value("hostname"), Certificate: value("certificate"), Key: value("key"), CA: value("ca"), AWGConfig: value("awg_config"), DNS: value("dns")}
			b, _ := json.Marshal(cfg)
			sink := &eventSink{ch: make(chan map[string]any, 200)}
			g := NewGateway(sink)
			if e = g.Start(string(b), nil); e != nil {
				t.Fatal(e)
			}
			defer g.Stop()
			timer := time.NewTimer(4 * time.Second)
			defer timer.Stop()
			echo := false
			transitEcho := false
		loop:
			for {
				select {
				case v := <-sink.ch:
					if v["event"] == "echo" {
						if !echo {
							t.Log("local RTT", v["rtt_ms"])
						}
						echo = true
					}
					if v["event"] == "transit_echo" {
						transitEcho = true
						t.Log("transit RTT", v["rtt_ms"])
					}
					if echo && transitEcho {
						break loop
					}
				case <-timer.C:
					break loop
				}
			}
			if !echo {
				t.Fatal("local RTT unavailable")
			}
			if transitEcho == (down || probeDown) {
				t.Fatalf("RTT availability wrong: echo=%v down=%v", echo, down)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			ip, e := fetchExitIP(ctx, func(ctx context.Context) (net.Conn, error) {
				s, e := g.dialStream(ctx, "exit-ip", "")
				if e != nil {
					return nil, e
				}
				return diagnosticConn{s}, nil
			})
			if down {
				probeCtx, probeCancel := context.WithTimeout(context.Background(), 3*time.Second)
				d := g.datagramBackend()
				if d == nil {
					t.Fatal("missing UDP backend")
				}
				udp, udpErr := d.DialContext(probeCtx, "udp4", net.JoinHostPort(value("dns"), "53"))
				if udpErr == nil {
					udp.SetDeadline(time.Now().Add(2 * time.Second))
					query := []byte{0x51, 0x4c, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
					udp.Write(query)
					b := make([]byte, 4096)
					_, udpErr = udp.Read(b)
					udp.Close()
					if udpErr == nil {
						probeCancel()
						t.Fatal("UDP direct fallback during transit outage")
					}
				}
				probeCancel()
				t.Log("UDP fail-closed passed")
			}
			if down {
				if e == nil {
					t.Fatal("direct egress during transit outage")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if expected != "" && ip != expected {
				t.Fatalf("unexpected egress %s", ip)
			}
			t.Log("transit exit", ip)
			d := g.datagramBackend()
			if d == nil {
				t.Fatal("UDP backend unavailable")
			}
			dns, e := d.DialContext(ctx, "udp4", net.JoinHostPort(value("dns"), "53"))
			if e != nil {
				t.Fatal(e)
			}
			defer dns.Close()
			dns.SetDeadline(time.Now().Add(3 * time.Second))
			query := []byte{0x51, 0x4c, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
			if _, e = dns.Write(query); e != nil {
				t.Fatal(e)
			}
			reply := make([]byte, 4096)
			n, e := dns.Read(reply)
			if e != nil || n < 12 || reply[0] != 0x51 || reply[1] != 0x4c || reply[2]&0x80 == 0 {
				t.Fatal("UDP transit DNS response", n, e)
			}
			t.Log("UDP through transit passed")

		})
	}
}
