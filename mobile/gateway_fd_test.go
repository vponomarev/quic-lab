//go:build linux

package mobile

import (
	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"sync/atomic"
	"testing"
)

type closingTestLink struct {
	stack.LinkEndpoint
	fd     int
	closes atomic.Int32
}

func (e *closingTestLink) Close() { e.closes.Add(1); unix.Close(e.fd) }
func TestOwnedLinkDoesNotCloseReusedDescriptor(t *testing.T) {
	fd, err := unix.Open("/dev/null", unix.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw := &closingTestLink{fd: fd}
	link := &ownedLinkEndpoint{LinkEndpoint: raw}
	link.Close()
	other, err := unix.Open("/dev/null", unix.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(other)
	if other != fd {
		if err := unix.Dup3(other, fd, 0); err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fd)
	}
	link.Close() // Stack.Wait removing the NIC after explicit shutdown.
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
		t.Fatalf("reused descriptor was closed: %v", err)
	}
	if raw.closes.Load() != 1 {
		t.Fatalf("close count: %d", raw.closes.Load())
	}
}
