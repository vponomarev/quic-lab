package mobile

import (
	"context"
	"errors"
	"net"
	"quiclab/internal/trafficbudget"
	"sync/atomic"
	"testing"
	"time"
)

type vlessTestBinder struct {
	calls atomic.Int32
	err   error
}

func (b *vlessTestBinder) Bind(int64) error { b.calls.Add(1); return b.err }
func TestVLESSSocketRefusesUnboundOrUnresolved(t *testing.T) {
	for _, endpoint := range []string{"server.invalid:443", "[::1]:443", "127.0.0.1:0"} {
		if _, err := newVLESSSocketFactory(endpoint, &vlessTestBinder{}); err == nil {
			t.Fatal("invalid endpoint admitted")
		}
	}
	if _, err := newVLESSSocketFactory("127.0.0.1:443", nil); err == nil {
		t.Fatal("missing binder admitted")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	sentinel := errors.New("protect failed")
	binder := &vlessTestBinder{err: sentinel}
	f, err := newVLESSSocketFactory(listener.Addr().String(), binder)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.DialContext(context.Background(), "tcp4", listener.Addr().String())
	if !errors.Is(err, sentinel) {
		t.Fatalf("binding failure ignored: %v", err)
	}
	if binder.calls.Load() != 1 {
		t.Fatal("binder not invoked exactly once")
	}
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(30 * time.Millisecond))
	if c, err := listener.Accept(); err == nil {
		c.Close()
		t.Fatal("unprotected connect occurred")
	}
	if _, err = f.DialContext(context.Background(), "tcp4", "127.0.0.1:1"); err == nil {
		t.Fatal("unconfigured target allowed")
	}
}
func TestVLESSOuterBudgetStopsActiveAndNewSockets(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	peer := make(chan net.Conn, 1)
	go func() {
		c, e := listener.Accept()
		if e == nil {
			peer <- c
		}
	}()
	budget, _ := NewTrafficBudget("vless", 8)
	binder := &vlessTestBinder{}
	f, err := newVLESSSocketFactory(listener.Addr().String(), budget.Bind(binder, "cell"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.DialContext(context.Background(), "tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := <-peer
	defer p.Close()
	if _, err = c.Write([]byte("12345678")); err != nil {
		t.Fatal(err)
	}
	if budget.CellAllowed() {
		t.Fatal("limit not enforced")
	}
	if budget.ledger.Snapshot().Used != 8 {
		t.Fatal("incorrect accounting")
	}
	if _, err = c.Write([]byte("9")); !errors.Is(err, trafficbudget.ErrBlocked) {
		t.Fatalf("write after limit: %v", err)
	}
	if _, err = f.DialContext(context.Background(), "tcp4", listener.Addr().String()); !errors.Is(err, trafficbudget.ErrBlocked) {
		t.Fatalf("new socket after limit: %v", err)
	}
}

func TestVLESSSocketConnectionState(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	f, _ := newVLESSSocketFactory(l.Addr().String(), &vlessTestBinder{})
	a, err := f.DialContext(context.Background(), "tcp4", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.DialContext(context.Background(), "tcp4", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if f.connectionState() != "established" {
		t.Fatal("TCP not reported")
	}
	a.Close()
	a.Close()
	if f.connectionState() != "established" {
		t.Fatal("sibling lost")
	}
	b.Close()
	if f.connectionState() != "closed" {
		t.Fatal("closed TCP still reported")
	}
	l.Close()
	_, err = f.DialContext(context.Background(), "tcp4", l.Addr().String())
	if err == nil {
		t.Fatal("expected failure")
	}
	if f.connectionState() != "error" {
		t.Fatal("failure not reported")
	}
}
