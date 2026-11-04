//go:build linux && !android

package awg

import (
	"errors"
	"github.com/amnezia-vpn/amneziawg-go/conn"
	"golang.org/x/sys/unix"
	"net"
	"net/netip"
	"sync"
)

// Linux desktop uses IPv4 only. Keep each receiver bound to its own socket
// generation and protect it through RawConn.Control, never a borrowed FD.
type linuxBind struct {
	mu     sync.Mutex
	socket *net.UDPConn
}

func newPlatformBind() conn.Bind    { return &linuxBind{} }
func (b *linuxBind) BatchSize() int { return 1 }
func (b *linuxBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.socket != nil {
		return nil, 0, conn.ErrBindAlreadyOpen
	}
	s, e := net.ListenUDP("udp4", &net.UDPAddr{Port: int(port)})
	if e != nil {
		return nil, 0, e
	}
	b.socket = s
	receive := func(bufs [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
		n, addr, e := s.ReadFromUDPAddrPort(bufs[0])
		if e != nil {
			return 0, e
		}
		sizes[0] = n
		eps[0] = &conn.StdNetEndpoint{AddrPort: addr}
		return 1, nil
	}
	return []conn.ReceiveFunc{receive}, uint16(s.LocalAddr().(*net.UDPAddr).Port), nil
}
func (b *linuxBind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.socket == nil {
		return nil
	}
	e := b.socket.Close()
	b.socket = nil
	return e
}
func (b *linuxBind) control(fn func(uintptr) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.socket == nil {
		return net.ErrClosed
	}
	raw, e := b.socket.SyscallConn()
	if e != nil {
		return e
	}
	var op error
	if e = raw.Control(func(fd uintptr) { op = fn(fd) }); e != nil {
		return e
	}
	return op
}
func (b *linuxBind) ProtectSockets(protect Binder) error {
	return b.control(func(fd uintptr) error { return protect.Bind(int64(fd)) })
}
func (b *linuxBind) SetMark(mark uint32) error {
	return b.control(func(fd uintptr) error { return unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark)) })
}
func (b *linuxBind) ParseEndpoint(s string) (conn.Endpoint, error) {
	a, e := netip.ParseAddrPort(s)
	if e != nil || !a.Addr().Is4() || a.Port() == 0 {
		return nil, errors.New("IPv4 AWG endpoint required")
	}
	return &conn.StdNetEndpoint{AddrPort: a}, nil
}
func (b *linuxBind) Send(bufs [][]byte, ep conn.Endpoint) error {
	endpoint, ok := ep.(*conn.StdNetEndpoint)
	if !ok || !endpoint.Addr().Is4() {
		return conn.ErrWrongEndpointType
	}
	b.mu.Lock()
	s := b.socket
	b.mu.Unlock()
	if s == nil {
		return net.ErrClosed
	}
	for _, buf := range bufs {
		if _, e := s.WriteToUDPAddrPort(buf, endpoint.AddrPort); e != nil {
			return e
		}
	}
	return nil
}
