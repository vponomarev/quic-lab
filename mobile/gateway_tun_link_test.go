package mobile

import (
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type trackedPacketLink struct {
	*channel.Endpoint
	closes      atomic.Int32
	waitEntered chan struct{}
	releaseWait chan struct{}
	once        sync.Once
}

func newTrackedPacketLink() *trackedPacketLink {
	return &trackedPacketLink{Endpoint: channel.New(16, 1280, "")}
}
func (e *trackedPacketLink) Close() { e.closes.Add(1); e.Endpoint.Close() }
func (e *trackedPacketLink) Wait() {
	if e.waitEntered != nil {
		e.once.Do(func() { close(e.waitEntered) })
		<-e.releaseWait
	}
	e.Endpoint.Wait()
}
func TestAttachLinkClosesOnce(t *testing.T) {
	g := NewGateway(nil)
	e := newTrackedPacketLink()
	if err := g.attachLink(e, nil); err != nil {
		t.Fatal(err)
	}
	g.Stop()
	g.Stop()
	if n := e.closes.Load(); n != 1 {
		t.Fatalf("closes=%d", n)
	}
}
func TestAttachLinkRejectsSecondAttach(t *testing.T) {
	g := NewGateway(nil)
	defer g.Stop()
	first, second := newTrackedPacketLink(), newTrackedPacketLink()
	if err := g.attachLink(first, nil); err != nil {
		t.Fatal(err)
	}
	if err := g.attachLink(second, nil); err == nil {
		t.Fatal("second attach accepted")
	}
	if first.closes.Load() != 0 {
		t.Fatal("active endpoint closed")
	}
	if second.closes.Load() != 1 {
		t.Fatal("rejected endpoint leaked")
	}
}
func TestAttachLinkFailureReleasesEndpoint(t *testing.T) {
	g := NewGateway(nil)
	defer g.Stop()
	e := newTrackedPacketLink()
	e.SetMTU(0)
	if err := g.attachLink(e, nil); err == nil {
		t.Fatal("zero MTU accepted")
	}
	if e.closes.Load() != 1 {
		t.Fatal("invalid endpoint leaked")
	}
	valid := newTrackedPacketLink()
	if err := g.attachLink(valid, nil); err != nil {
		t.Fatalf("failure prevented retry: %v", err)
	}
}
func TestAttachLinkStopWaitsWorkers(t *testing.T) {
	g := NewGateway(nil)
	e := newTrackedPacketLink()
	e.waitEntered = make(chan struct{})
	e.releaseWait = make(chan struct{})
	if err := g.attachLink(e, nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { g.Stop(); close(done) }()
	select {
	case <-e.waitEntered:
	case <-time.After(3 * time.Second):
		close(e.releaseWait)
		t.Fatal("endpoint wait not reached")
	}
	select {
	case <-done:
		close(e.releaseWait)
		t.Fatal("stop returned before endpoint worker")
	default:
	}
	close(e.releaseWait)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not finish")
	}
	if e.closes.Load() != 1 {
		t.Fatal("endpoint closed more than once")
	}
}
