package mobile

import (
	"errors"
	"net"
	"sync/atomic"
	"syscall"
	"time"
)

// pathSocket treats an unavailable network as lost UDP packets, rather than a
// fatal error for every QUIC connection registered on the socket. QUIC ACKs still
// determine delivery, recovery and congestion control. Idle/write timeouts remain
// in force if no replacement path works. Other errors are never suppressed.
// Deliberately expose only PacketConn: the optimized syscall/batch path would
// bypass ReadFrom. Suitable for the lab's echo workload, not throughput testing.
type pathSocket struct {
	net.PacketConn
	unavailable   atomic.Bool
	onUnavailable func(net.Addr, error)
}

func networkUnavailable(err error) bool {
	return errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.ENETDOWN) || errors.Is(err, syscall.EHOSTUNREACH)
}

func (s *pathSocket) report(err error) {
	if s.unavailable.CompareAndSwap(false, true) && s.onUnavailable != nil {
		s.onUnavailable(s.LocalAddr(), err)
	}
}

func (s *pathSocket) WriteTo(b []byte, addr net.Addr) (int, error) {
	n, err := s.PacketConn.WriteTo(b, addr)
	if networkUnavailable(err) {
		s.report(err)
		return len(b), nil // Datagram was lost; never an acknowledgement of delivery.
	}
	if err == nil {
		s.unavailable.Store(false)
	}
	return n, err
}

func (s *pathSocket) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		n, addr, err := s.PacketConn.ReadFrom(b)
		if !networkUnavailable(err) {
			return n, addr, err
		}
		s.report(err)
		// An old unavailable path may remain registered after migration. Don't
		// terminate the migrated connection; Close/deadlines still interrupt reads.
		time.Sleep(25 * time.Millisecond)
	}
}
