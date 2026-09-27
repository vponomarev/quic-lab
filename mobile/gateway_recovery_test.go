package mobile

import (
	"context"
	"encoding/json"
	"github.com/quic-go/quic-go"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"quiclab/internal/gateway"
	"quiclab/internal/protocol"
	"testing"
	"time"
)

func TestVPNReconnectAfterServerRestartPreservesTUN(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(ca, []byte(cp), 0600)
	tc, e := gateway.TLS(pair, ca)
	if e != nil {
		t.Fatal(e)
	}
	srv, _ := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	addr := "127.0.0.1:0"
	start := func() (*quic.Listener, context.CancelFunc) {
		l, e := quic.ListenAddr(addr, tc, &quic.Config{EnableDatagrams: true})
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithCancel(context.Background())
		go srv.ServeQUIC(ctx, l)
		return l, cancel
	}
	l, cancel := start()
	addr = l.Addr().String()
	defer func() { cancel(); l.Close() }()
	g := NewGateway(nil)
	cfg, _ := json.Marshal(gatewayConfig{Transport: "quic", Endpoint: addr, Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
	if e = g.Start(string(cfg), nil); e != nil {
		t.Fatal(e)
	}
	defer g.Stop()
	stopped := false
	g.tunStop = func() { stopped = true }
	for i := 0; i < 2; i++ {
		cancel()
		l.Close()
		until := time.Now().Add(3 * time.Second)
		for g.IsConnected() && time.Now().Before(until) {
			time.Sleep(10 * time.Millisecond)
		}
		if g.IsConnected() {
			t.Fatal("dead transport reported alive")
		}
		l, cancel = start()
		if e = g.Reconnect(nil); e != nil {
			t.Fatal(e)
		}
		if stopped || !g.IsConnected() {
			t.Fatal("recovery dropped TUN or failed")
		}
		ctx, cancelFlow := context.WithTimeout(context.Background(), 2*time.Second)
		s, e := gateway.Open(ctx, g.open, "echo", "")
		if e != nil {
			cancelFlow()
			t.Fatal(e)
		}
		s.SetDeadline(time.Now().Add(time.Second))
		json.NewEncoder(s).Encode(protocol.Frame{Seq: 42})
		var frame protocol.Frame
		e = json.NewDecoder(s).Decode(&frame)
		s.Close()
		cancelFlow()
		if e != nil || frame.Seq != 42 {
			t.Fatal("new flow after recovery", e)
		}
	}
}
