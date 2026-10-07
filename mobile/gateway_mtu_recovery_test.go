package mobile

import (
	"context"
	"encoding/json"
	"github.com/quic-go/quic-go"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"quiclab/internal/gateway"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// An LTE cell/path change can reduce MTU without changing Android Network or
// the outer socket. Small heartbeat replies must not mask stuck application data.
func TestQUICDataAfterSamePathMTUReduction(t *testing.T) {
	if os.Getenv("QUIC_MTU_EXPERIMENT") != "1" {
		t.Skip("Investigative reproducer: same-path MTU loss, not confirmed road incident; opt in with QUIC_MTU_EXPERIMENT=1")
	}
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(ca, []byte(cp), 0600)
	tlsConfig, e := gateway.TLS(pair, ca)
	if e != nil {
		t.Fatal(e)
	}
	server, e := quic.ListenAddr("127.0.0.1:0", tlsConfig, &quic.Config{EnableDatagrams: true})
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, _ := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	go srv.ServeQUIC(ctx, server)
	proxy, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if e != nil {
		t.Fatal(e)
	}
	defer proxy.Close()
	upstream := server.Addr().(*net.UDPAddr)
	var maxSize atomic.Int64
	maxSize.Store(65535)
	var dropped atomic.Int64
	var peer *net.UDPAddr
	var mu sync.Mutex
	go func() {
		b := make([]byte, 65535)
		for {
			n, from, e := proxy.ReadFromUDP(b)
			if e != nil {
				return
			}
			mu.Lock()
			to := upstream
			if from.String() == upstream.String() {
				to = peer
			} else {
				peer = from
			}
			mu.Unlock()
			if int64(n) > maxSize.Load() {
				dropped.Add(1)
				continue
			}
			if to != nil {
				proxy.WriteToUDP(b[:n], to)
			}
		}
	}()
	tcp, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer tcp.Close()
	go func() {
		for {
			c, e := tcp.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	sink := &eventSink{ch: make(chan map[string]any, 10000)}
	g := NewGateway(sink)
	defer g.Stop()
	cfg, _ := json.Marshal(gatewayConfig{Transport: "quic", Endpoint: proxy.LocalAddr().String(), Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
	if e := g.Start(string(cfg), nil); e != nil {
		t.Fatal(e)
	}
	st, e := gateway.Open(ctx, g.open, "tcp", tcp.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	transfer := func() error {
		st.SetDeadline(time.Now().Add(4 * time.Second))
		data := make([]byte, 128*1024)
		done := make(chan error, 1)
		go func() { _, e := st.Write(data); done <- e }()
		_, e := io.ReadFull(st, data)
		if e != nil {
			return e
		}
		return <-done
	}
	if e := transfer(); e != nil {
		t.Fatal("warmup", e)
	}
	time.Sleep(400 * time.Millisecond) // allow MTU discovery on the initial path
	maxSize.Store(1280)
	if e := transfer(); e != nil {
		// Record whether the existing echo stream remains healthy after large data stalls.
		for len(sink.ch) > 0 {
			<-sink.ch
		}
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		healthy := false
	loop:
		for {
			select {
			case ev := <-sink.ch:
				if ev["event"] == "echo" {
					healthy = true
					break loop
				}
			case <-timer.C:
				break loop
			}
		}
		t.Fatalf("large transfer stuck after MTU reduction: %v; heartbeat alive=%v; dropped=%d", e, healthy, dropped.Load())
	}
}
