package windowsengine

import (
	"quiclab/internal/approuting"
	"sync"
	"testing"
)

type testAppCounters struct{}

func (testAppCounters) Snapshot() approuting.Statistics {
	return approuting.Statistics{Enabled: true, DirectTxBytes: 10}
}
func TestAppRoutingStatusConcurrentRestart(t *testing.T) {
	var s appRoutingState
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			s.Set(testAppCounters{})
			s.Final(approuting.Statistics{Enabled: true, DirectTxBytes: 10})
			s.Set(nil)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			if v := s.Snapshot(); v != nil {
				v.DirectTxBytes = 999
			}
		}
	}()
	wg.Wait()
	s.Final(approuting.Statistics{DirectTxBytes: 10})
	value := s.Snapshot()
	value.DirectTxBytes = 999
	if s.Snapshot().DirectTxBytes != 10 {
		t.Fatal("mutable status escaped")
	}
}
