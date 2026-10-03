package bond

import (
	"context"
	"time"
)

// SetTerminationHandler attaches the authenticated server registry owner.
// The handler must remove the session from its registry before returning.
func (s *Session) SetTerminationHandler(handler func()) {
	s.mu.Lock()
	s.terminateHandler = handler
	s.mu.Unlock()
}

// Terminate notifies a reachable peer of an intentional whole-session stop.
// Close alone preserves the peer's disconnect grace for accidental path loss.
// All I/O is nonblocking and the caller bounds the acknowledgement wait.
func (s *Session) Terminate(ctx context.Context) {
	s.mu.Lock()
	if len(s.paths) == 0 || s.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	if s.terminateAck == nil {
		s.terminateAck = make(chan struct{})
	}
	ack := s.terminateAck
	s.mu.Unlock()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.mu.Lock()
		for name, p := range s.paths {
			_ = s.sendOn(name, p, frame(terminate, 0, 0, 0, 0, false, nil), true)
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-s.ctx.Done():
			return
		case <-ack:
			return
		case <-ticker.C:
		}
	}
}
