package mobile

import (
	"bytes"
	"quiclab/internal/gateway"
	"sync/atomic"
	"testing"
)

type trafficTestStream struct {
	gateway.Stream
	bytes.Buffer
}

func (s *trafficTestStream) Read(p []byte) (int, error)  { return s.Buffer.Read(p) }
func (s *trafficTestStream) Write(p []byte) (int, error) { return s.Buffer.Write(p) }
func TestTrafficCountsActualBytes(t *testing.T) {
	var tx, rx atomic.Int64
	raw := &trafficTestStream{}
	s := trafficStream{Stream: raw, tx: &tx, rx: &rx}
	s.Write([]byte("hello"))
	b := make([]byte, 3)
	s.Read(b)
	s.Read(b)
	s.Read(b)
	if tx.Load() != 5 || rx.Load() != 5 {
		t.Fatalf("tx=%d rx=%d", tx.Load(), rx.Load())
	}
}
