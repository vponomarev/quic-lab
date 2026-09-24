package quic

import "github.com/quic-go/quic-go/internal/wire"

// PATH_RESPONSE isn't retransmitted on its own: the peer retries its challenge.
// Probe loss processing requires a handler even for these non-retransmitted frames.
type pathResponseHandler struct{}

func (pathResponseHandler) OnAcked(wire.Frame) {}
func (pathResponseHandler) OnLost(wire.Frame)  {}
