package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCaptureBondLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cert := &x509.Certificate{Raw: []byte("capture-device")}
	cs := tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
	server, setupErr := New("0.0.0.0/0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if setupErr != nil {
		t.Fatal(setupErr)
	}
	var starts, stops, captures atomic.Int32
	field := reflect.ValueOf(server).Elem().FieldByName("Multiplexed")
	if !field.IsValid() {
		t.Fatal("gateway lacks explicit multiplexed lifecycle hook")
	}
	field.Set(reflect.ValueOf(func(tls.ConnectionState) (func(), error) { starts.Add(1); return func() { stops.Add(1) }, nil }))
	server.Capture = func(tls.ConnectionState, string, string, net.Conn) net.Conn { captures.Add(1); return nil }
	entry, token, err := server.joinBond(ctx, ctx, cs, BondHello{Token: strings.Repeat("ab", 32), Path: "wifi", Create: true}, "quic", "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 {
		t.Fatal("bond not registered before serve")
	}
	if _, _, err := server.joinBond(ctx, ctx, cs, BondHello{Token: token, Path: "cell"}, "https", "127.0.0.1:2"); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 {
		t.Fatal("path join duplicated logical session")
	}
	captureConnection(entry.mux.Context(), "tcp", "203.0.113.1:443", nil)
	entry.mux.Close()
	deadline := time.Now().Add(time.Second)
	for stops.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if stops.Load() != 1 {
		t.Fatal("bond end did not release capture eligibility")
	}
	if captures.Load() != 0 {
		t.Fatal("bond exposed standalone capture wrapper")
	}
}
