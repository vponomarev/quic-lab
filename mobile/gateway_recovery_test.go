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
		pc, e := net.ListenPacket("udp4", addr)
		if e != nil {
			t.Fatal(e)
		}
		tr := &quic.Transport{Conn: pc}
		l, e := tr.Listen(tc, &quic.Config{EnableDatagrams: true})
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithCancel(context.Background())
		go srv.ServeQUIC(ctx, l)
		// A process restart closes the UDP socket, not only the QUIC listener.
		return l, func() { cancel(); l.Close(); tr.Close(); pc.Close() }
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
		// An abrupt UDP socket close may lose CONNECTION_CLOSE. Raw QUIC keeps
		// its 90-second idle timeout; Android's independent health policy
		// detects silent loss and requests Reconnect. This test supplies that
		// recovery decision and checks transport replacement preserves TUN.
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
