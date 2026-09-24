package mobile

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type failingPacketConn struct {
	net.PacketConn
	failed    atomic.Bool
	dropReads bool
}

func (p *failingPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		n, a, err := p.PacketConn.ReadFrom(b)
		if err != nil || (!p.failed.Load() || !p.dropReads) {
			return n, a, err
		}
		// A removed network also cannot deliver packets from the server.
	}
}

func (p *failingPacketConn) WriteTo(b []byte, a net.Addr) (int, error) {
	if p.failed.Load() {
		return 0, &net.OpError{Op: "write", Err: syscall.ENETUNREACH}
	}
	return p.PacketConn.WriteTo(b, a)
}

func TestAbruptPathLossKeepsConnection(t *testing.T) {
	for _, dropReads := range []bool{false, true} {
		t.Run(fmt.Sprint("dropReads=", dropReads), func(t *testing.T) {
			addr, pin := testServer(t)
			sink := &eventSink{ch: make(chan map[string]any, 2048)}
			c := NewClient(sink)
			defer c.Stop()
			var failed *failingPacketConn
			c.newTransport = func(ip net.IP, b SocketBinder, cb func(net.Addr, error)) (transport, error) {
				tr, err := openTransport(ip, b, cb)
				if err == nil && failed == nil {
					socket := tr.q.Conn.(*pathSocket)
					failed = &failingPacketConn{PacketConn: socket.PacketConn, dropReads: dropReads}
					socket.PacketConn = failed
				}
				return tr, err
			}
			if err := c.Start(addr, "", pin, 20, nil); err != nil {
				t.Fatal(err)
			}
			first := nextEcho(t, sink, func(map[string]any) bool { return true })
			lostAt := time.Now()
			failed.failed.Store(true)
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
		waiting:
			for {
				select {
				case e := <-sink.ch:
					if e["event"] == "disconnected" {
						t.Fatalf("connection died: %v", e)
					}
					if e["event"] == "path_unavailable" {
						break waiting
					}
				case <-timer.C:
					t.Fatal("missing path loss event")
				}
			}
			if err := c.Migrate(nil); err != nil {
				t.Fatal(err)
			}
			next := nextEcho(t, sink, func(e map[string]any) bool { return e["peer"] != first["peer"] })
			if next["connection_id"] != first["connection_id"] || next["stream_id"] != first["stream_id"] {
				t.Fatal("reconnected", first, next)
			}
			if next["seq"].(float64) <= first["seq"].(float64) {
				t.Fatal("sequence did not advance")
			}
			if time.Since(lostAt) > 700*time.Millisecond {
				t.Fatalf("migration recovery too slow: %v", time.Since(lostAt))
			}
		})
	}
}

func TestOnlyNetworkLossIsSuppressed(t *testing.T) {
	for _, err := range []error{syscall.ENETUNREACH, syscall.ENETDOWN, syscall.EHOSTUNREACH} {
		if !networkUnavailable(&net.OpError{Err: err}) {
			t.Fatal(err)
		}
	}
	for _, err := range []error{net.ErrClosed, syscall.EACCES, errors.New("other"), nil} {
		if networkUnavailable(err) {
			t.Fatal("suppressed unrelated error", err)
		}
	}
}
