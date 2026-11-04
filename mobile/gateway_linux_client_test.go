//go:build linux && !android

package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestLinuxStandaloneVLESSAndCancellation(t *testing.T) {
	raw := vlessGatewayJSON(t)
	g := NewGateway(nil)
	if e := g.StartLinuxContext(context.Background(), raw, gatewayVLESSBinder{}); e != nil {
		t.Fatal(e)
	}
	if !g.IsConnected() {
		t.Fatal("engine not started")
	}
	g.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := g.StartLinuxContext(ctx, raw, gatewayVLESSBinder{}); !errors.Is(e, context.Canceled) {
		t.Fatal("cancellation not preserved", e)
	}
	var cfg map[string]any
	json.Unmarshal([]byte(raw), &cfg)
	cfg["ca"] = "synthetic-invalid-root"
	bad, _ := json.Marshal(cfg)
	if e := g.StartLinuxContext(context.Background(), string(bad), gatewayVLESSBinder{}); e == nil {
		g.Stop()
		t.Fatal("invalid desktop TLS trust ignored")
	}
}
