//go:build linux && !android

package awg

import (
	"errors"
	"github.com/amnezia-vpn/amneziawg-go/conn"
	"golang.org/x/sys/unix"
	"net"
	"testing"
	"time"
)

type socketCheck struct {
	calls int
	fail  bool
}

func (b *socketCheck) Bind(fd int64) error {
	b.calls++
	if b.fail {
		return errors.New("denied")
	}
	_, e := unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_TYPE)
	return e
}
func TestLinuxBoundAWGSocketLifecycle(t *testing.T) {
	check := &socketCheck{}
	b := &protectedBind{Bind: newPlatformBind(), binder: check}
	receive, port, e := b.Open(0)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if check.calls != 1 {
		t.Fatalf("socket protections=%d", check.calls)
	}
	peer, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if e != nil {
		t.Fatal(e)
	}
	defer peer.Close()
	peer.SetDeadline(time.Now().Add(3 * time.Second))
	ep, e := b.ParseEndpoint(peer.LocalAddr().String())
	if e != nil {
		t.Fatal(e)
	}
	if e = b.Send([][]byte{[]byte("ping")}, ep); e != nil {
		t.Fatal(e)
	}
	buf := make([]byte, 64)
	n, a, e := peer.ReadFromUDP(buf)
	if e != nil || string(buf[:n]) != "ping" || a.Port != int(port) {
		t.Fatal("send", e)
	}
	peer.WriteToUDP([]byte("pong"), a)
	sizes := make([]int, 1)
	eps := make([]conn.Endpoint, 1)
	n, e = receive[0]([][]byte{buf}, sizes, eps)
	if e != nil || n != 1 || string(buf[:sizes[0]]) != "pong" {
		t.Fatal("receive", e)
	}
	b.Close()
	if _, e = receive[0]([][]byte{buf}, sizes, eps); !errors.Is(e, net.ErrClosed) {
		t.Fatal("old receiver not closed", e)
	}
	if _, _, e = b.Open(0); e != nil {
		t.Fatal("reopen", e)
	}
	b.Close()
	check.fail = true
	if _, _, e = b.Open(0); e == nil {
		t.Fatal("failed protection accepted")
	}
	check.fail = false
	if _, _, e = b.Open(0); e != nil {
		t.Fatal("failed-start socket leaked", e)
	}
}
