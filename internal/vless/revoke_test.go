package vless

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/common/uuid"
	"github.com/xtls/xray-core/features/routing"
	account "github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/transport"
)

type blockingDispatcher struct{ entered chan string }

func (d *blockingDispatcher) Type() interface{} { return routing.DispatcherType() }
func (d *blockingDispatcher) Start() error      { return nil }
func (d *blockingDispatcher) Close() error      { return nil }
func (d *blockingDispatcher) Dispatch(context.Context, xnet.Destination) (*transport.Link, error) {
	return nil, errors.New("unused")
}
func (d *blockingDispatcher) DispatchLink(ctx context.Context, _ xnet.Destination, _ *transport.Link) error {
	d.entered <- session.InboundFromContext(ctx).User.Email
	<-ctx.Done()
	return ctx.Err()
}
func authenticatedContext(t *testing.T, id string, c net.Conn) context.Context {
	t.Helper()
	u, err := uuid.ParseString(id)
	if err != nil {
		t.Fatal(err)
	}
	return session.ContextWithInbound(context.Background(), &session.Inbound{Name: "vless", Conn: c, User: &protocol.MemoryUser{Email: id, Account: &account.MemoryAccount{ID: protocol.NewID(u)}}})
}
func TestServerRevokeClosesActiveFlows(t *testing.T) {
	base := &blockingDispatcher{entered: make(chan string, 2)}
	g := newDeviceDispatcher(base)
	defer g.Close()
	ids := []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"}
	done := []chan error{make(chan error, 1), make(chan error, 1)}
	peers := make([]net.Conn, 2)
	for i, id := range ids {
		c, p := net.Pipe()
		peers[i] = p
		defer p.Close()
		ctx := authenticatedContext(t, id, c)
		go func(i int) { done[i] <- g.DispatchLink(ctx, xnet.TCPDestination(xnet.LocalHostIP, 80), nil) }(i)
	}
	for range ids {
		select {
		case <-base.entered:
		case <-time.After(time.Second):
			t.Fatal("flow not dispatched")
		}
	}
	g.Revoke(ids[0])
	select {
	case <-done[0]:
	case <-time.After(time.Second):
		t.Fatal("revoked flow active")
	}
	select {
	case <-done[1]:
		t.Fatal("other device interrupted")
	default:
	}
	peers[0].SetReadDeadline(time.Now().Add(time.Second))
	_, err := peers[0].Read(make([]byte, 1))
	if err == nil {
		t.Fatal("outer connection not closed")
	}
	if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("outer connection leaked")
	}
	c, p := net.Pipe()
	defer p.Close()
	err = g.DispatchLink(authenticatedContext(t, ids[0], c), xnet.TCPDestination(xnet.LocalHostIP, 80), nil)
	if !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("revoked admission %v", err)
	}
	g.Close()
	select {
	case <-done[1]:
	case <-time.After(time.Second):
		t.Fatal("shutdown leaked flow")
	}
}
func TestServerRejectsUnauthenticatedDispatcher(t *testing.T) {
	g := newDeviceDispatcher(&blockingDispatcher{entered: make(chan string, 1)})
	defer g.Close()
	if err := g.DispatchLink(context.Background(), xnet.TCPDestination(xnet.LocalHostIP, 80), nil); err == nil {
		t.Fatal("anonymous admission")
	}
}
func TestServerRevokeCanonicalIdentity(t *testing.T) {
	base := &blockingDispatcher{entered: make(chan string, 1)}
	g := newDeviceDispatcher(base)
	defer g.Close()
	g.Revoke("AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA")
	c, p := net.Pipe()
	defer p.Close()
	ctx, cancel := context.WithTimeout(authenticatedContext(t, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", c), 100*time.Millisecond)
	defer cancel()
	if e := g.DispatchLink(ctx, xnet.TCPDestination(xnet.LocalHostIP, 80), nil); !errors.Is(e, ErrDeviceRevoked) {
		t.Fatal("non-canonical revoke bypassed")
	}
}
