//go:build linux

package mobile

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device/fdbased"
	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// Feed real app-side TCP packets into the mobile TUN adapter. Confirms that a
// destination RST is not merely visible at the QUIC stream API boundary.
func TestTUNQUICDestinationResetUnblocksApplication(t *testing.T) {
	testTUNQUICFailureUnblocksApplication(t, false)
}

func TestTUNQUICReconnectUnblocksApplication(t *testing.T) {
	testTUNQUICFailureUnblocksApplication(t, true)
}

func testTUNQUICFailureUnblocksApplication(t *testing.T, reconnect bool) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan *net.TCPConn, 1)
	go func() {
		c, e := ln.Accept()
		if e == nil {
			accepted <- c.(*net.TCPConn)
		}
	}()
	g := diagnosticQUICGateway(t, func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp4", ln.Addr().String())
	})
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fds[0])
	defer unix.Close(fds[1])
	if err = g.Attach(fds[0]); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Dup(fds[1])
	if err != nil {
		t.Fatal(err)
	}
	dev, err := fdbased.Open(strconv.Itoa(fd), 1280, 0)
	if err != nil {
		unix.Close(fd)
		t.Fatal(err)
	}
	link := &ownedLinkEndpoint{LinkEndpoint: dev}
	cs, err := core.CreateStack(&core.Config{LinkEndpoint: link, TransportHandler: rejectInbound{}})
	if err != nil {
		link.Close()
		t.Fatal(err)
	}
	defer func() { cs.Close(); link.Close(); cs.Wait() }()
	var nic tcpip.NICID
	for id := range cs.NICInfo() {
		nic = id
		break
	}
	if e := cs.AddProtocolAddress(nic, tcpip.ProtocolAddress{Protocol: ipv4.ProtocolNumber, AddressWithPrefix: tcpip.AddrFrom4([4]byte{10, 254, 254, 1}).WithPrefix()}, stack.AddressProperties{}); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	app, err := gonet.DialContextTCP(ctx, cs, tcpip.FullAddress{NIC: nic, Addr: tcpip.AddrFrom4([4]byte{192, 0, 2, 1}), Port: 443}, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var remote *net.TCPConn
	select {
	case remote = <-accepted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer remote.Close()
	if reconnect {
		if err = g.Reconnect(nil); err != nil {
			t.Fatal(err)
		}
	} else {
		if err = remote.SetLinger(0); err != nil {
			t.Fatal(err)
		}
		remote.Close()
	}
	app.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = app.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("application read survived connection failure")
	}
	if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatalf("application did not receive close: %v", err)
	}
	t.Logf("Connection failure (reconnect=%t) unblocked app-side TCP read: %v", reconnect, err)
}
