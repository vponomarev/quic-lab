package mobile

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/quic-go/quic-go"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"quiclab/internal/gateway"
	"testing"
	"time"
)

func TestGatewayUDPDatagrams(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(ca, []byte(cp), 0600)
	tc, _ := gateway.TLS(pair, ca)
	srv, _ := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, e := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{EnableDatagrams: true, MaxIncomingStreams: 128})
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	go srv.ServeQUIC(ctx, ln)
	target, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	go func() {
		b := make([]byte, 65535)
		for {
			n, a, e := target.ReadFrom(b)
			if e != nil {
				return
			}
			if string(b[:n]) == "drop" {
				continue
			}
			target.WriteTo(b[:n], a)
			if string(b[:n]) == "push" {
				target.WriteTo([]byte("unsolicited"), a)
			}
		}
	}()
	g := NewGateway(nil)
	cfg, _ := json.Marshal(gatewayConfig{Transport: "quic", Endpoint: ln.Addr().String(), Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
	if e = g.Start(string(cfg), nil); e != nil {
		t.Fatal(e)
	}
	defer g.Stop()
	d := g.datagramBackend()
	if d == nil {
		t.Fatal("datagrams not negotiated")
	}
	flow, e := d.DialContext(ctx, "udp4", target.LocalAddr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer flow.Close()
	flow.SetDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 65535)
	for _, p := range [][]byte{[]byte("small"), bytes.Repeat([]byte{42}, 1252), bytes.Repeat([]byte{43}, 4000), {}} {
		if _, e = flow.Write(p); e != nil {
			t.Fatal(e)
		}
		n, e := flow.Read(buf)
		if e != nil || !bytes.Equal(buf[:n], p) {
			t.Fatalf("datagram boundary %d %v", n, e)
		}
	}
	flow.Write([]byte("drop"))
	flow.Write([]byte("push"))
	for _, want := range []string{"push", "unsolicited"} {
		n, e := flow.Read(buf)
		if e != nil || string(buf[:n]) != want {
			t.Fatalf("UDP request/reply serialization: %q %v", buf[:n], e)
		}
	}
	// Migration retains connection, control stream and UDP socket at the exit.
	if e = g.MigrateTo("test-new-path", nil); e != nil {
		t.Fatal(e)
	}
	flow.Write([]byte("after-migration"))
	n, e := flow.Read(buf)
	if e != nil || string(buf[:n]) != "after-migration" {
		t.Fatal("UDP flow lost on migration", e)
	}
	// Destination policy also applies to datagrams.
	if other, e := d.DialContext(ctx, "udp4", "192.0.2.1:9999"); e == nil {
		other.Close()
		t.Fatal("UDP bypassed destination ACL")
	}
}

func TestGatewayTCPInteractiveLatency(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(ca, []byte(cp), 0600)
	tc, _ := gateway.TLS(pair, ca)
	srv, _ := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, e := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{EnableDatagrams: true, MaxIncomingStreams: 128})
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	go srv.ServeQUIC(ctx, ln)
	target, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	go func() {
		c, e := target.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		io.Copy(c, c)
	}()
	g := NewGateway(nil)
	cfg, _ := json.Marshal(gatewayConfig{Transport: "quic", Endpoint: ln.Addr().String(), Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
	if e = g.Start(string(cfg), nil); e != nil {
		t.Fatal(e)
	}
	defer g.Stop()
	c, e := g.dialStream(ctx, "tcp", target.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	var worst, total time.Duration
	for i := 0; i < 40; i++ {
		start := time.Now()
		c.Write([]byte{1})
		c.Write([]byte{2})
		var b [2]byte
		if _, e := io.ReadFull(c, b[:]); e != nil {
			t.Fatal(e)
		}
		elapsed := time.Since(start)
		total += elapsed
		worst = max(worst, elapsed)
		time.Sleep(5 * time.Millisecond)
	}
	t.Logf("TCP tiny writes: mean=%s max=%s", total/40, worst)
	if worst > 200*time.Millisecond {
		t.Fatal("unexpected interactive TCP stall", worst)
	}
}
