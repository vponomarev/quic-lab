//go:build linux

package vless

import (
	"context"
	"net"
	"os"
	"quiclab/internal/serverexit"
	"sync/atomic"
	"testing"
)

type brokerExitFixture struct{ net.Dialer }

func (*brokerExitFixture) Resolver() *net.Resolver     { return net.DefaultResolver }
func (*brokerExitFixture) Probe(context.Context) error { return nil }
func (*brokerExitFixture) ProbeICMP(context.Context, []byte) ([]byte, error) {
	return nil, serverexit.ErrDenied
}
func (*brokerExitFixture) Close() {}

func realExitBroker(t *testing.T) (func(context.Context, string, string, string) (net.Conn, error), *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	seen := new(atomic.Int32)
	broker := serverexit.Broker{Acquire: func(protocol, id string, close func()) (serverexit.Lease, error) {
		if protocol != "vless" || (id != serverID && id != otherServerID) {
			return serverexit.Lease{}, serverexit.ErrDenied
		}
		seen.Add(1)
		return serverexit.Lease{Provider: &brokerExitFixture{}, Release: func() {}}, nil
	}}
	stop, err := broker.Serve(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	return func(ctx context.Context, id, network, address string) (net.Conn, error) {
		return (serverexit.Client{Dir: dir}).Dial(ctx, "vless", id, network, address)
	}, seen
}
func TestServerRealityVisionExitBroker(t *testing.T) {
	dial, seen := realExitBroker(t)
	testServerRealityVision(t, false, dial)
	if seen.Load() != 2 {
		t.Fatal("REALITY bypassed exit broker", seen.Load())
	}
}
func TestServerTLSExitBrokerTCPUDP(t *testing.T) {
	dial, seen := realExitBroker(t)
	cert, key := fixtureCertificate(t)
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	server, err := StartServer(context.Background(), ServerOptions{Listen: addr, Security: "tls", Mode: "standalone", Certificate: cert, Key: key, Clients: []ServerClient{{UUID: serverID}, {UUID: otherServerID}}, ExitDial: dial})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	for _, id := range []string{serverID, otherServerID} {
		for _, network := range []string{"tcp4", "udp4"} {
			target := tcpEcho(t)
			if network == "udp4" {
				target = udpEcho(t)
			}
			client := serverClient(t, addr, cert, id)
			c, err := client.DialContext(context.Background(), network, target)
			if err != nil {
				t.Fatal(err)
			}
			exchange(t, c)
			c.Close()
		}
	}
	if seen.Load() != 4 {
		t.Fatal("TLS bypassed exit broker", seen.Load())
	}
}
