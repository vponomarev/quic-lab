package mobile

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"github.com/quic-go/quic-go"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"quiclab/internal/gateway"
	"testing"
	"time"
)

func TestGatewayRTTToggleWithoutReconnect(t *testing.T) {
	for _, mode := range []string{"quic", "https"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			pair, cp, kp := testIdentity(t)
			ca := filepath.Join(t.TempDir(), "ca.pem")
			os.WriteFile(ca, []byte(cp), 0600)
			tc, e := gateway.TLS(pair, ca)
			if e != nil {
				t.Fatal(e)
			}
			srv, _ := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var addr string
			if mode == "quic" {
				ln, e := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{})
				if e != nil {
					t.Fatal(e)
				}
				defer ln.Close()
				addr = ln.Addr().String()
				go srv.ServeQUIC(ctx, ln)
			} else {
				cfg := tc.Clone()
				cfg.NextProtos = []string{"http/1.1"}
				ln, e := tls.Listen("tcp", "127.0.0.1:0", cfg)
				if e != nil {
					t.Fatal(e)
				}
				hs := &http.Server{Handler: http.HandlerFunc(srv.WebSocket)}
				defer hs.Close()
				addr = ln.Addr().String()
				go hs.Serve(ln)
			}
			sink := &policySink{make(chan map[string]any, 256)}
			g := NewGateway(sink)
			g.SetRTT(false, 1000)
			cfg, _ := json.Marshal(gatewayConfig{Transport: mode, Endpoint: addr, Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
			if e := g.Start(string(cfg), nil); e != nil {
				t.Fatal(e)
			}
			defer g.Stop()
			connected := 0
			await := func(want string, limit time.Duration) {
				t.Helper()
				timer := time.NewTimer(limit)
				defer timer.Stop()
				for {
					select {
					case e := <-sink.events:
						kind := e["event"]
						if kind == "connected" {
							connected++
							if connected > 1 {
								t.Fatal("unexpected reconnect")
							}
						}
						if kind == "disconnected" {
							t.Fatal(e)
						}
						if want == "health" && kind == "echo" {
							t.Fatal("disabled RTT leaked")
						}
						if kind == want {
							return
						}
					case <-timer.C:
						t.Fatalf("missing %s", want)
					}
				}
			}
			await("health", 7*time.Second)
			await("health", 7*time.Second)
			await("health", 7*time.Second) // past the old 15-second HTTPS read deadline
			g.SetRTT(true, 1000)
			await("echo", 3*time.Second)
			g.SetRTT(false, 1000)
			await("health", 7*time.Second)
			if !g.IsConnected() {
				t.Fatal("transport lost")
			}
		})
	}
}
