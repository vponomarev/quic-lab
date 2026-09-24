package mobile

import (
	"quiclab/internal/gateway"
	"sync/atomic"
)

// Counts tunnel application bytes, excluding handshake, echo and transport overhead.
type trafficStream struct {
	gateway.Stream
	tx, rx *atomic.Int64
}

func (s trafficStream) Write(p []byte) (int, error) {
	n, e := s.Stream.Write(p)
	s.tx.Add(int64(n))
	return n, e
}
func (s trafficStream) Read(p []byte) (int, error) {
	n, e := s.Stream.Read(p)
	s.rx.Add(int64(n))
	return n, e
}
