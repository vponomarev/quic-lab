//go:build linux && !android

package linuxclient

import (
	"sync"
	"testing"
)

type eventRecorder struct {
	mu     sync.Mutex
	values []string
}

func (r *eventRecorder) OnEvent(v string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = append(r.values, v)
}

func TestRetiredGatewayCannotPublishLateHealth(t *testing.T) {
	r := &eventRecorder{}
	old := &gatewayEvents{sink: r}
	old.OnEvent("old echo")
	old.retire()
	next := &gatewayEvents{sink: r}
	next.OnEvent("new connected")
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); old.OnEvent("late echo"); old.retire() }()
	}
	workers.Wait()
	next.OnEvent("new echo")
	if len(r.values) != 3 || r.values[2] != "new echo" {
		t.Fatal(r.values)
	}
}
