package mobile

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/tun"
	"github.com/quic-go/quic-go"
	"quiclab/internal/awg/netstack"
	"quiclab/internal/gateway"
)

func diagnosticQUICGateway(t *testing.T, dial func(context.Context, string, string) (net.Conn, error)) *Gateway {
	t.Helper()
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, []byte(cp), 0600); err != nil {
		t.Fatal(err)
	}
	tc, err := gateway.TLS(pair, ca)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := gateway.New("192.0.2.0/24", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	srv.DialContext = dial
	ln, err := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{MaxIncomingStreams: 128})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); ln.Close() })
	go srv.ServeQUIC(ctx, ln)
	g := NewGateway(nil)
	raw, _ := json.Marshal(gatewayConfig{Transport: "quic", Endpoint: ln.Addr().String(), Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
	if err := g.Start(string(raw), nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Stop)
	return g
}

// A real RST from the destination must end the proxied read, even if the
// client has not half-closed its request side. No production network is used.
func TestQUICDestinationResetReachesStream(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	peer := make(chan *net.TCPConn, 1)
	go func() {
		c, e := ln.Accept()
		if e == nil {
			peer <- c.(*net.TCPConn)
		}
	}()
	g := diagnosticQUICGateway(t, func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp4", ln.Addr().String())
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := gateway.Open(ctx, g.open, "tcp", "192.0.2.1:443")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var c *net.TCPConn
	select {
	case c = <-peer:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err = c.SetLinger(0); err != nil {
		t.Fatal(err)
	}
	c.Close()
	s.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = s.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("read survived upstream reset")
	}
	if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatalf("reset not propagated: %v", err)
	}
	t.Logf("Upstream RST reached client stream as %T: %v; QUIC connected=%v", err, err, g.IsConnected())
}

// Model loss of one NAT/remote TCP state: drop packets of an established tuple,
// while allowing new tuples to the same server. Uses the actual AWG userspace
// TCP stack, but in-memory packets instead of AWG encryption or host firewall.
func TestQUICHealthyWhileExistingEgressTCPBlackholed(t *testing.T) {
	a, an, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.55.0.2")}, nil, 1280)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, bn, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.55.0.1")}, nil, 1280)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var lostPort atomic.Uint32
	var dropped atomic.Uint64
	pump := func(src, dst tun.Device) {
		buf := make([]byte, 1600)
		sizes := make([]int, 1)
		for {
			if _, e := src.Read([][]byte{buf}, sizes, 0); e != nil {
				return
			}
			p := buf[:sizes[0]]
			port := lostPort.Load()
			if len(p) >= 40 && p[9] == 6 && port != 0 {
				off := int(p[0]&15) * 4
				if len(p) >= off+4 && (uint32(binary.BigEndian.Uint16(p[off:off+2])) == port || uint32(binary.BigEndian.Uint16(p[off+2:off+4])) == port) {
					dropped.Add(1)
					continue
				}
			}
			if _, e := dst.Write([][]byte{p}, 0); e != nil {
				return
			}
		}
	}
	go pump(a, b)
	go pump(b, a)
	ln, err := bn.ListenTCPAddrPort(netip.MustParseAddrPort("10.55.0.1:8080"))
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
	var firstPort atomic.Uint32
	g := diagnosticQUICGateway(t, func(ctx context.Context, _, _ string) (net.Conn, error) {
		c, e := an.DialContext(ctx, "tcp4", "10.55.0.1:8080")
		if e == nil {
			firstPort.CompareAndSwap(0, uint32(c.LocalAddr().(*net.TCPAddr).Port))
		}
		return c, e
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	open := func() gateway.Stream {
		s, e := gateway.Open(ctx, g.open, "tcp", "192.0.2.1:443")
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	exchange := func(s gateway.Stream) {
		s.SetDeadline(time.Now().Add(2 * time.Second))
		if _, e := s.Write([]byte("ping")); e != nil {
			t.Fatal(e)
		}
		buf := make([]byte, 4)
		if _, e := io.ReadFull(s, buf); e != nil || string(buf) != "ping" {
			t.Fatalf("exchange: %q %v", buf, e)
		}
	}
	old := open()
	exchange(old)
	lostPort.Store(firstPort.Load())
	old.SetDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err = old.Write([]byte("lost request")); err != nil {
		t.Fatal(err)
	}
	_, err = old.Read(make([]byte, 1))
	if e, ok := err.(net.Error); !ok || !e.Timeout() {
		t.Fatalf("expected silent flow stall, got %v", err)
	}
	if dropped.Load() == 0 {
		t.Fatal("fixture did not drop an old-flow packet")
	}
	fresh := open()
	exchange(fresh)
	if !g.IsConnected() {
		t.Fatal("QUIC unexpectedly disconnected")
	}
	t.Logf("Old TCP flow stalled with %d packets dropped; same QUIC session and new flow to same destination work", dropped.Load())
}
