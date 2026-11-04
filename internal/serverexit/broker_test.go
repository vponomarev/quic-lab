//go:build linux

package serverexit

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type brokerProvider struct{ fakeProvider }

// Close interrupts reads, but a write already in progress must finish before
// revocation can acknowledge that the old provider is no longer used.
type pausedExitConn struct {
	net.Conn
	entered, resume chan struct{}
}

func (c *pausedExitConn) Write(b []byte) (int, error) {
	close(c.entered)
	<-c.resume
	return 0, net.ErrClosed
}

type pausedExitProvider struct {
	fakeProvider
	conn net.Conn
}

func (p *pausedExitProvider) DialContext(context.Context, string, string) (net.Conn, error) {
	return p.conn, nil
}

func TestBrokerRevocationJoinsOutboundWrite(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	a, peer := net.Pipe()
	defer peer.Close()
	out := &pausedExitConn{Conn: a, entered: make(chan struct{}), resume: make(chan struct{})}
	var resume sync.Once
	unpause := func() { resume.Do(func() { close(out.resume) }) }
	defer unpause()
	registered := make(chan func(), 1)
	b := Broker{Acquire: func(_, _ string, revoke func()) (Lease, error) {
		registered <- revoke
		return Lease{Provider: &pausedExitProvider{conn: out}, Release: func() {}}, nil
	}}
	stop, err := b.Serve(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	// Release the paused writer before stopping, including on test failure.
	defer func() { unpause(); stop() }()
	c, err := (Client{Dir: dir}).Dial(context.Background(), "vless", "valid", "tcp4", "192.0.2.1:80")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("old exit"))
	select {
	case <-out.entered:
	case <-time.After(time.Second):
		t.Fatal("write not entered")
	}
	revoke := <-registered
	done := make(chan struct{})
	go func() { revoke(); close(done) }()
	select {
	case <-done:
		t.Fatal("revocation acknowledged while old exit write is running")
	case <-time.After(50 * time.Millisecond):
	}
	unpause()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("revocation did not finish after write cleanup")
	}
}

func (*brokerProvider) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	left, right := net.Pipe()
	go func() {
		defer right.Close()
		b := make([]byte, 65535)
		for {
			n, e := right.Read(b)
			if e != nil {
				return
			}
			if _, e = right.Write(b[:n]); e != nil {
				return
			}
		}
	}()
	return left, nil
}
func TestBrokerAuthenticationFramingAndShutdown(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	var releases atomic.Int32
	b := Broker{Acquire: func(protocol, identity string, close func()) (Lease, error) {
		if protocol != "vless" || identity != "valid" {
			return Lease{}, ErrDenied
		}
		return Lease{Provider: &brokerProvider{}, Release: func() { releases.Add(1) }}, nil
	}}
	stop, err := b.Serve(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	client := Client{Dir: dir}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := client.Dial(ctx, "vless", "forged", "tcp4", "192.0.2.1:80"); err == nil {
		t.Fatal("unauthenticated dial")
	}
	for _, network := range []string{"tcp4", "udp4"} {
		c, err := client.Dial(ctx, "vless", "valid", network, "192.0.2.1:80")
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(time.Second))
		if _, err = c.Write([]byte("first")); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 5)
		if _, err = io.ReadFull(c, buf); err != nil || string(buf) != "first" {
			t.Fatal(err, string(buf))
		}
		if _, err = c.Write([]byte("second")); err != nil {
			t.Fatal(err)
		}
		buf = make([]byte, 6)
		if _, err = io.ReadFull(c, buf); err != nil || string(buf) != "second" {
			t.Fatal(err, string(buf))
		}
		c.Close()
	}
	c, err := client.Dial(ctx, "vless", "valid", "tcp4", "192.0.2.1:80")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	stop()
	c.SetReadDeadline(time.Now().Add(time.Second))
	var buf [1]byte
	if _, err = c.Read(buf[:]); err == nil {
		t.Fatal("stop left flow alive")
	}
	if releases.Load() != 3 {
		t.Fatal("leases leaked", releases.Load())
	}
}
func TestBrokerRejectsForeignPathsAndDuplicateOwner(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	b := Broker{Acquire: func(string, string, func()) (Lease, error) { return Lease{}, errors.New("denied") }}
	stop, err := b.Serve(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := b.Serve(context.Background(), dir); err == nil {
		other()
		t.Fatal("duplicate owner")
	}
	stop()
	path := filepath.Join(dir, brokerName)
	if err = os.WriteFile(path, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if stop, err := b.Serve(context.Background(), dir); err == nil {
		stop()
		t.Fatal("foreign file removed")
	}
	if raw, _ := os.ReadFile(path); string(raw) != "preserve" {
		t.Fatal("foreign file changed")
	}
}

func TestBrokerTCPHalfClosePreservesResponse(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		io.ReadAll(c)
		c.Write([]byte("response after EOF"))
	}()
	b := Broker{Acquire: func(string, string, func()) (Lease, error) {
		return Lease{Provider: &packetTestProvider{tcp: l.Addr().String()}, Release: func() {}}, nil
	}}
	stop, err := b.Serve(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	c, err := (Client{Dir: dir}).Dial(context.Background(), "vless", "valid", "tcp4", "192.0.2.1:80")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	c.Write([]byte("request"))
	c.(interface{ CloseWrite() error }).CloseWrite()
	raw, err := io.ReadAll(c)
	if err != nil || string(raw) != "response after EOF" {
		t.Fatal(string(raw), err)
	}
}
