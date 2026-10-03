package vless

import (
	"context"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/features/policy"
	"io"
	"net"
	"testing"
	"time"
)

func TestServerHalfClosePreservesLastResponse(t *testing.T) {
	_, addr, cert := serverFixture(t, "standalone", "")
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		b := make([]byte, 7)
		io.ReadFull(c, b)
		c.Write(append([]byte("response:"), b...))
	}()
	c, e := serverClient(t, addr, cert, serverID).DialContext(context.Background(), "tcp4", l.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	c.Write([]byte("request"))
	c.(interface{ CloseWrite() error }).CloseWrite()
	b, e := io.ReadAll(c)
	if e != nil || string(b) != "response:request" {
		t.Fatalf("last response lost: %q %v", b, e)
	}
}
func TestServerPolicyRequiresAuthenticatedContext(t *testing.T) {
	d := &serverPolicyDispatcher{server: &serverContext{mode: "standalone"}}
	dest := xnet.UDPDestination(xnet.LocalHostIP, 1234)
	for _, ctx := range []context.Context{context.Background(), context.WithValue(context.Background(), admittedContextKey{}, true), session.ContextWithInbound(context.Background(), &session.Inbound{Name: "vless"})} {
		if _, e := d.Dispatch(ctx, dest); e == nil {
			t.Fatal("unauthenticated UDP")
		}
		if e := d.DispatchLink(ctx, dest, nil); e == nil {
			t.Fatal("unauthenticated TCP")
		}
	}
}
func TestServerPolicyPinsDestination(t *testing.T) {
	p := &serverContext{mode: "demux-only", target: "127.0.0.1:2443"}
	for _, s := range []string{"tcp:127.0.0.2:2443", "tcp:127.0.0.1:2444", "tcp:localhost:2443", "udp:127.0.0.1:2443"} {
		d, _ := xnet.ParseDestination(s)
		if p.permits(d) {
			t.Fatal("destination escaped pin")
		}
	}
}
func TestServerBoundedQueue(t *testing.T) {
	ctx := serverDispatchContext(context.Background())
	if n := policy.BufferPolicyFromContext(ctx).PerConnection; n <= 0 || n > 64*1024 {
		t.Fatalf("unbounded or excessive queue: %d", n)
	}
}
