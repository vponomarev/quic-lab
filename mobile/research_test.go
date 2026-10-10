package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"quiclab/internal/gateway"
	"quiclab/internal/research"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type researchTestObserver struct {
	allowed atomic.Bool
	owners  atomic.Int64
	gate    chan struct{}
}

func (o *researchTestObserver) Allowed() bool { return o.allowed.Load() }
func (o *researchTestObserver) Owner(int64, string, int64, string, int64) string {
	o.owners.Add(1)
	if o.gate != nil {
		<-o.gate
	}
	return `{"id":"124","uid":"123","packages":["test.app"],"labels":["Test"],"attribution":"known"}`
}
func researchTestCollector(t *testing.T, o *researchTestObserver, send researchExchange) *researchCollector {
	t.Helper()
	c := newResearchCollector(o, send)
	t.Cleanup(c.stop)
	return c
}
func TestResearchRequiresBothConsentAndServerPolicy(t *testing.T) {
	o := &researchTestObserver{}
	var batches []gateway.ResearchBatch
	server := false
	c := researchTestCollector(t, o, func(_ context.Context, b gateway.ResearchBatch) (gateway.ResearchReply, error) {
		batches = append(batches, b)
		return gateway.ResearchReply{Allowed: server}, nil
	})
	f := c.openFlow(6, "10.0.0.2", 1200, "1.2.3.4", 443)
	f.add(100, 0, 0, 0)
	c.sync(time.Now())
	if c.collecting() {
		t.Fatal("default must deny")
	}
	o.allowed.Store(true)
	c.changed()
	c.sync(time.Now())
	f.add(200, 0, 0, 0)
	if c.collecting() || o.owners.Load() != 0 {
		t.Fatal("server deny must suppress collection and owner lookup")
	}
	server = true
	c.sync(time.Now())
	if !c.collecting() {
		t.Fatal("dual grant ignored")
	}
	f.add(7, 11, 0, 0)
	f.close()
	c.flush(time.Now().Add(2 * time.Second))
	c.sync(time.Now())
	var up, down uint64
	for _, b := range batches {
		for _, r := range b.Records {
			if r.Type == "flow_close" {
				var d research.FlowData
				json.Unmarshal(r.Data, &d)
				up += d.BytesUp
				down += d.BytesDown
				if !d.Incomplete {
					t.Fatal("preexisting flow must be incomplete")
				}
			}
		}
	}
	if up != 7 || down != 11 {
		t.Fatalf("retrospective or missing counters: %d/%d", up, down)
	}
}
func TestResearchRevocationDropsPendingAndLateOwner(t *testing.T) {
	o := &researchTestObserver{gate: make(chan struct{})}
	o.allowed.Store(true)
	defer close(o.gate)
	var batches []gateway.ResearchBatch
	c := researchTestCollector(t, o, func(_ context.Context, b gateway.ResearchBatch) (gateway.ResearchReply, error) {
		batches = append(batches, b)
		return gateway.ResearchReply{Allowed: true}, nil
	})
	c.sync(time.Now())
	f := c.openFlow(17, "10.0.0.2", 1200, "1.2.3.4", 53)
	f.add(999, 111, 1, 1)
	o.allowed.Store(false)
	c.changed()
	f.close()
	c.flush(time.Now().Add(2 * time.Second))
	c.sync(time.Now())
	last := batches[len(batches)-1]
	if last.Enabled || len(last.Records) > 0 || c.collecting() {
		t.Fatal("revoke must discard pending data")
	}
}
func TestResearchLateGrantCannotUndoRevoke(t *testing.T) {
	o := &researchTestObserver{}
	o.allowed.Store(true)
	entered, release := make(chan struct{}), make(chan struct{})
	c := researchTestCollector(t, o, func(_ context.Context, b gateway.ResearchBatch) (gateway.ResearchReply, error) {
		close(entered)
		<-release
		return gateway.ResearchReply{Allowed: true}, nil
	})
	done := make(chan struct{})
	go func() { c.sync(time.Now()); close(done) }()
	<-entered
	o.allowed.Store(false)
	c.changed()
	close(release)
	<-done
	if c.collecting() {
		t.Fatal("stale grant revived consent")
	}
}
func TestResearchIntervalsAreDeltasAndFailuresRequireResync(t *testing.T) {
	o := &researchTestObserver{}
	o.allowed.Store(true)
	var records []research.Record
	fail := false
	c := researchTestCollector(t, o, func(_ context.Context, b gateway.ResearchBatch) (gateway.ResearchReply, error) {
		records = append(records, b.Records...)
		if fail {
			return gateway.ResearchReply{}, errors.New("offline")
		}
		return gateway.ResearchReply{Allowed: true}, nil
	})
	now := time.Now()
	c.sync(now)
	f := c.openFlow(6, "10.0.0.2", 1200, "1.2.3.4", 443)
	f.add(13, 17, 0, 0)
	c.flush(now.Add(61 * time.Second))
	c.sync(now.Add(61 * time.Second))
	f.add(19, 23, 0, 0)
	f.close()
	c.flush(now.Add(63 * time.Second))
	c.sync(now.Add(63 * time.Second))
	var up, down uint64
	var delta, closed bool
	for _, r := range records {
		if r.Type == "flow_delta" || r.Type == "flow_close" {
			var d research.FlowData
			json.Unmarshal(r.Data, &d)
			up += d.BytesUp
			down += d.BytesDown
			delta = delta || r.Type == "flow_delta"
			closed = closed || r.Type == "flow_close"
		}
	}
	if up != 32 || down != 40 || !delta || !closed {
		t.Fatalf("wrong interval totals: %d/%d", up, down)
	}
	fail = true
	c.sync(now)
	if c.collecting() {
		t.Fatal("connection failure must close collection gate")
	}
}
func TestResearchQueueBoundAndSlowOwnerDoNotBlockTraffic(t *testing.T) {
	o := &researchTestObserver{gate: make(chan struct{})}
	o.allowed.Store(true)
	defer close(o.gate)
	c := researchTestCollector(t, o, func(_ context.Context, b gateway.ResearchBatch) (gateway.ResearchReply, error) {
		return gateway.ResearchReply{Allowed: true}, nil
	})
	c.sync(time.Now())
	done := make(chan struct{})
	go func() {
		for i := 0; i < 2000; i++ {
			f := c.openFlow(6, "10.0.0.2", 1200, "1.2.3.4", 443)
			f.add(1, 1, 0, 0)
			f.close()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("slow owner blocked traffic")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.queue) > researchQueueRecords || c.queueBytes > researchQueueBytes || c.dropped == 0 {
		t.Fatal("unbounded queue or unreported loss")
	}
}
func TestResearchConcurrentCounterRevocation(t *testing.T) {
	o := &researchTestObserver{}
	o.allowed.Store(true)
	c := researchTestCollector(t, o, func(_ context.Context, b gateway.ResearchBatch) (gateway.ResearchReply, error) {
		return gateway.ResearchReply{Allowed: true}, nil
	})
	c.sync(time.Now())
	f := c.openFlow(6, "10.0.0.2", 123, "1.2.3.4", 443)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				f.add(1, 2, 0, 0)
			}
		}()
	}
	o.allowed.Store(false)
	c.changed()
	wg.Wait()
	f.close()
	if c.collecting() {
		t.Fatal("revocation did not stick")
	}
}

func TestResearchFullQueuePreservesAggregatedDropCount(t *testing.T) {
	o := &researchTestObserver{gate: make(chan struct{})}
	o.allowed.Store(true)
	defer close(o.gate)
	c := researchTestCollector(t, o, func(_ context.Context, b gateway.ResearchBatch) (gateway.ResearchReply, error) {
		return gateway.ResearchReply{Allowed: true, Accepted: len(b.Records)}, nil
	})
	c.sync(time.Now())
	c.mu.Lock()
	for i := 0; i < researchQueueRecords+100; i++ {
		c.enqueueLocked("gap", 0, map[string]any{"reason": "test"}, time.Now())
	}
	before := c.dropped
	c.mu.Unlock()
	c.sync(time.Now())
	c.mu.Lock()
	after := c.dropped
	c.mu.Unlock()
	if after < before {
		t.Fatalf("lost gap count in full queue: before=%d after=%d", before, after)
	}
}
func TestResearchShortFlowCloseGetsAsynchronousOwner(t *testing.T) {
	o := &researchTestObserver{gate: make(chan struct{})}
	o.allowed.Store(true)
	c := researchTestCollector(t, o, func(_ context.Context, b gateway.ResearchBatch) (gateway.ResearchReply, error) {
		return gateway.ResearchReply{Allowed: true, Accepted: len(b.Records)}, nil
	})
	c.sync(time.Now())
	f := c.openFlow(6, "10.0.0.2", 123, "1.2.3.4", 443)
	f.add(9, 13, 0, 0)
	f.close()
	close(o.gate)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		found := false
		for _, item := range c.queue {
			if item.record.Type == "flow_close" {
				var data research.FlowData
				json.Unmarshal(item.record.Data, &data)
				found = data.App.UID == 123 && data.BytesUp == 9 && data.BytesDown == 13
			}
		}
		c.mu.Unlock()
		if found {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("short-flow close did not receive resolved owner")
}

func TestResearchDefaultOffNeverContactsServer(t *testing.T) {
	o := &researchTestObserver{}
	calls := 0
	c := researchTestCollector(t, o, func(context.Context, gateway.ResearchBatch) (gateway.ResearchReply, error) {
		calls++
		return gateway.ResearchReply{}, nil
	})
	for i := 0; i < 5; i++ {
		c.sync(time.Now().Add(time.Duration(i) * researchPolicyInterval))
	}
	// A local toggle that never reached the server also needs no revocation exchange.
	o.allowed.Store(true)
	c.changed()
	o.allowed.Store(false)
	c.changed()
	c.sync(time.Now())
	c.reset()
	c.sync(time.Now())
	if calls != 0 {
		t.Fatalf("default/off-only session contacted server %d times", calls)
	}
}
func TestResearchRevocationRetriesUntilAcknowledgedThenStaysQuiet(t *testing.T) {
	o := &researchTestObserver{}
	o.allowed.Store(true)
	var batches []gateway.ResearchBatch
	revokeFailures := 2
	c := researchTestCollector(t, o, func(_ context.Context, b gateway.ResearchBatch) (gateway.ResearchReply, error) {
		batches = append(batches, b)
		if !b.Enabled && revokeFailures > 0 {
			revokeFailures--
			return gateway.ResearchReply{}, errors.New("reply lost")
		}
		return gateway.ResearchReply{Allowed: b.Enabled, Accepted: len(b.Records)}, nil
	})
	c.sync(time.Now())
	o.allowed.Store(false)
	c.changed()
	for i := 0; i < 6; i++ {
		c.sync(time.Now().Add(time.Duration(i) * researchPolicyInterval))
	}
	if len(batches) != 4 {
		t.Fatalf("expected grant plus 3 revoke attempts, got %d", len(batches))
	}
	for _, b := range batches[1:] {
		if b.Enabled || len(b.Records) > 0 || b.Generation != batches[1].Generation {
			t.Fatal("revocation retries must be empty and idempotent")
		}
	}
	o.allowed.Store(true)
	c.changed()
	c.sync(time.Now())
	if len(batches) != 5 || !batches[4].Enabled {
		t.Fatal("later explicit consent did not restart negotiation")
	}
}
func TestResearchCanceledGrantStillRequiresRevocation(t *testing.T) {
	o := &researchTestObserver{}
	o.allowed.Store(true)
	entered, release := make(chan struct{}), make(chan struct{})
	var batches []gateway.ResearchBatch
	c := researchTestCollector(t, o, func(ctx context.Context, b gateway.ResearchBatch) (gateway.ResearchReply, error) {
		batches = append(batches, b)
		if b.Enabled {
			close(entered)
			<-release
			return gateway.ResearchReply{}, ctx.Err()
		}
		return gateway.ResearchReply{}, nil
	})
	done := make(chan struct{})
	go func() { c.sync(time.Now()); close(done) }()
	<-entered
	o.allowed.Store(false)
	c.changed()
	close(release)
	<-done
	c.sync(time.Now())
	c.sync(time.Now().Add(researchPolicyInterval))
	if len(batches) != 2 || batches[1].Enabled || batches[1].Generation <= batches[0].Generation {
		t.Fatal("in-flight grant must be revoked even without its acknowledgement")
	}
}

// The second read captures the old grant and returns it only after a newer
// notification has synchronously applied revocation.
type researchSamplingObserver struct {
	reads          atomic.Int64
	oldReadStarted chan struct{}
	releaseOldRead chan struct{}
}

func (o *researchSamplingObserver) Allowed() bool {
	switch o.reads.Add(1) {
	case 1:
		return true
	case 2:
		close(o.oldReadStarted)
		<-o.releaseOldRead
		return true
	default:
		return false
	}
}
func (o *researchSamplingObserver) Owner(int64, string, int64, string, int64) string { return "" }
func TestResearchStaleConsentSampleCannotUndoSynchronousRevocation(t *testing.T) {
	o := &researchSamplingObserver{oldReadStarted: make(chan struct{}), releaseOldRead: make(chan struct{})}
	c := newResearchCollector(o, func(_ context.Context, b gateway.ResearchBatch) (gateway.ResearchReply, error) {
		return gateway.ResearchReply{Allowed: b.Enabled, Accepted: len(b.Records)}, nil
	})
	t.Cleanup(c.stop)
	var once sync.Once
	release := func() { once.Do(func() { close(o.releaseOldRead) }) }
	defer release()
	c.sync(time.Now())
	if !c.collecting() {
		t.Fatal("initial grant not active")
	}
	olderDone := make(chan struct{})
	go func() { c.changed(); close(olderDone) }()
	select {
	case <-o.oldReadStarted:
	case <-time.After(time.Second):
		t.Fatal("old sample did not start")
	}
	revokeDone := make(chan struct{})
	go func() { c.changed(); close(revokeDone) }()
	select {
	case <-revokeDone:
	case <-time.After(time.Second):
		t.Fatal("revocation blocked behind slow consent observer")
	}
	c.mu.Lock()
	revokedGeneration := c.generation
	revoked := !c.consent && !c.active
	c.mu.Unlock()
	if !revoked {
		t.Fatal("revocation was not synchronous")
	}
	release()
	select {
	case <-olderDone:
	case <-time.After(time.Second):
		t.Fatal("old sample did not complete")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.consent || c.active || c.generation != revokedGeneration {
		t.Fatal("stale true sample restored consent after a newer revoke")
	}
}
