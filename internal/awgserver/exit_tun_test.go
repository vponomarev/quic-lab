package awgserver

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"
)

type exitTestTUN struct {
	admissionTestTUN
	in, out chan []byte
	done    chan struct{}
	once    sync.Once
}

func (t *exitTestTUN) Close() error { t.once.Do(func() { close(t.done) }); return nil }
func (t *exitTestTUN) Write(bufs [][]byte, offset int) (int, error) {
	for _, b := range bufs {
		t.in <- append([]byte(nil), b[offset:]...)
	}
	return len(bufs), nil
}
func (t *exitTestTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	select {
	case <-t.done:
		return 0, net.ErrClosed
	case p := <-t.out:
		sizes[0] = copy(bufs[0][offset:], p)
		return 1, nil
	}
}
func TestExitTUNRoutesOnlyAuthorizedManagedPeers(t *testing.T) {
	kernel := &exitTestTUN{in: make(chan []byte, 8), out: make(chan []byte, 8), done: make(chan struct{})}
	opened := make(chan string, 8)
	ended := make(chan struct{}, 8)
	r := NewExitTUN(kernel, func(ctx context.Context, id string) (net.Conn, error) {
		opened <- id
		if id == "offline" {
			return nil, errors.New("broker down")
		}
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			defer func() { ended <- struct{}{} }()
			p := make([]byte, 65535)
			for {
				n, err := b.Read(p)
				if err != nil {
					return
				}
				reply := append([]byte(nil), p[:n]...)
				copy(reply[12:16], p[16:20])
				copy(reply[16:20], p[12:16])
				if _, err = b.Write(reply); err != nil {
					return
				}
			}
		}()
		return a, nil
	})
	defer r.Close()
	a, b, c := netip.MustParseAddr("10.77.0.2"), netip.MustParseAddr("10.77.0.3"), netip.MustParseAddr("10.77.0.4")
	r.Update(map[netip.Addr]ExitRoute{a: {ID: "legacy"}, b: {ID: "named", Managed: true, Revision: "one"}, c: {ID: "offline", Managed: true}})
	r.Write([][]byte{admissionPacket(a.String(), "192.0.2.1")}, 0)
	select {
	case <-kernel.in:
	case <-time.After(time.Second):
		t.Fatal("legacy traffic lost")
	}
	r.Write([][]byte{admissionPacket(b.String(), "192.0.2.1")}, 0)
	select {
	case id := <-opened:
		if id != "named" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("named peer not routed")
	}
	result := make(chan string, 1)
	go func() {
		buf := make([]byte, 2048)
		sizes := []int{0}
		n, err := r.Read([][]byte{buf}, sizes, 0)
		if err != nil || n != 1 {
			result <- "error"
			return
		}
		dst, _ := packetAddress(buf[:sizes[0]], false)
		result <- dst.String()
	}()
	select {
	case dst := <-result:
		if dst != b.String() {
			t.Fatal(dst)
		}
	case <-time.After(time.Second):
		t.Fatal("broker reply lost")
	}
	r.Write([][]byte{admissionPacket(c.String(), "192.0.2.1"), admissionPacket("10.77.0.9", "192.0.2.1")}, 0)
	select {
	case <-opened:
	case <-time.After(time.Second):
		t.Fatal("offline broker not tried")
	}
	select {
	case <-kernel.in:
		t.Fatal("managed or unknown peer fell back to kernel")
	default:
	}
	r.Update(map[netip.Addr]ExitRoute{a: {ID: "legacy"}, b: {ID: "named"}})
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("old managed flow remained open")
	}
	r.Write([][]byte{admissionPacket(b.String(), "192.0.2.1")}, 0)
	select {
	case <-kernel.in:
	case <-time.After(time.Second):
		t.Fatal("explicit legacy restoration failed")
	}
}
