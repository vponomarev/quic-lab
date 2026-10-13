//go:build linux

package vpnclienttest

import (
	"context"
	"errors"
	"quiclab/internal/vpnclient"
	"quiclab/internal/vpnmodel"
	"testing"
)

func config() vpnclient.Config {
	return vpnclient.Config{Revision: 1, Profiles: vpnmodel.Config{Version: 1, Exits: []vpnmodel.Exit{{ID: "e", Name: "test", Kind: "standalone"}}, Profiles: []vpnmodel.Profile{{ID: "p", ExitID: "e", Transport: "quic", Mode: "auto", Endpoint: "127.0.0.1:443", PoolSize: 1}}}}
}
func TestFactoryFailureIsSafe(t *testing.T) {
	b := New("qlprobe", `{"transport":"quic","endpoint":"127.0.0.1:443"}`)
	b.open = func(string) (device, error) { return nil, errors.New("private config") }
	c := vpnclient.NewController(b)
	if c.Start(context.Background(), config()) != vpnclient.ErrStart {
		t.Fatal("unsafe error")
	}
	if c.Snapshot().Reason != "backend_start_failed" {
		t.Fatal(c.Snapshot())
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestCancelledBackendDoesNotOpenDevice(t *testing.T) {
	b := New("qlprobe", `{}`)
	called := false
	b.open = func(string) (device, error) { called = true; return nil, errors.New("unexpected") }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if b.Start(ctx, config()) == nil || called {
		t.Fatal("cancelled startup touched TUN")
	}
}
