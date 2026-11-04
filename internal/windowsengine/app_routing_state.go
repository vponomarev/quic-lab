package windowsengine

import (
	"quiclab/internal/approuting"
	"sync"
)

// appRoutingState publishes a live counter source or its final value to status
// polling independently of native gateway lifecycle operations.
type appCounterSource interface{ Snapshot() approuting.Statistics }
type appRoutingState struct {
	mu     sync.Mutex
	source appCounterSource
	final  *approuting.Statistics
}

func (s *appRoutingState) Set(source appCounterSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.source = source
	s.final = nil
}
func (s *appRoutingState) Final(value approuting.Statistics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.final = &value
	s.source = nil
}
func (s *appRoutingState) Snapshot() *approuting.Statistics {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.source != nil {
		value := s.source.Snapshot()
		return &value
	}
	if s.final != nil {
		value := *s.final
		return &value
	}
	return nil
}
