package mobile

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"quiclab/internal/gateway"
)

// Opt-in LAN fixture for the real Android worker. Fresh synthetic credentials,
// ephemeral listeners, no TUN, no existing server configuration or user profiles.
func TestPathProbePhoneFixture(t *testing.T) {
	bind, output := os.Getenv("PATH_PROBE_FIXTURE_ADDR"), os.Getenv("PATH_PROBE_FIXTURE_OUTPUT")
	if bind == "" || output == "" {
		t.Skip("explicit LAN fixture address and output required")
	}
	if ip := net.ParseIP(bind); ip == nil || !ip.IsPrivate() {
		t.Fatal("fixture requires a private LAN address")
	}
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, []byte(cp), 0600); err != nil {
		t.Fatal(err)
	}
	tc, err := gateway.TLS(pair, ca)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	var configs []gatewayConfig
	for _, mode := range []string{"quic", "https"} {
		srv, err := gateway.New("0.0.0.0/0", slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		if mode == "quic" {
			srv.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				select {
				case <-time.After(1500 * time.Millisecond):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				return (&net.Dialer{}).DialContext(ctx, network, address)
			}
		}
		var endpoint string
		if mode == "quic" {
			listener, err := quic.ListenAddr(net.JoinHostPort(bind, "0"), tc, &quic.Config{MaxIncomingStreams: 128})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			endpoint = listener.Addr().String()
			go srv.ServeQUIC(ctx, listener)
		} else {
			cfg := tc.Clone()
			cfg.NextProtos = []string{"http/1.1"}
			listener, err := tls.Listen("tcp4", net.JoinHostPort(bind, "0"), cfg)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(srv.WebSocket), ReadHeaderTimeout: 5 * time.Second}
			defer server.Close()
			endpoint = listener.Addr().String()
			go server.Serve(listener)
		}
		configs = append(configs, gatewayConfig{Transport: mode, Endpoint: endpoint, Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
	}
	raw, err := json.Marshal(configs)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, raw, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(output)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := os.Stat(output + ".stop"); err == nil {
				return
			}
		}
	}
}
