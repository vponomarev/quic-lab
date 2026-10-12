package mobile

import "quiclab/internal/gateway"

// This wrapper sees application payload once, before transport framing/retransmission.
type researchStream struct {
	gateway.Stream
	flow *researchFlow
}

func (s researchStream) Write(p []byte) (int, error) {
	n, err := s.Stream.Write(p)
	s.flow.add(uint64(n), 0, 0, 0)
	if n > 0 {
		s.flow.tlsPayload(p[:n])
	}
	return n, err
}
func (s researchStream) Read(p []byte) (int, error) {
	n, err := s.Stream.Read(p)
	s.flow.add(0, uint64(n), 0, 0)
	return n, err
}

func (f *researchFlow) tlsPayload(b []byte) {
	if f == nil {
		return
	}
	c := f.collector
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active || c.stopped || f.closed || !f.recording || (c.eligible != nil && !c.eligible(f)) {
		return
	}
	f.tls.Feed(b)
}
