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
	return n, err
}
func (s researchStream) Read(p []byte) (int, error) {
	n, err := s.Stream.Read(p)
	s.flow.add(0, uint64(n), 0, 0)
	return n, err
}
