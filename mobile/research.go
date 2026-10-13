package mobile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"quiclab/internal/gateway"
	"quiclab/internal/netobserve"
	"quiclab/internal/research"
)

// ResearchObserver is explicit device consent plus optional Android flow ownership.
// Owner runs on a bounded worker, never on the forwarding goroutine.
type ResearchObserver interface {
	OwnerForUID(uid int64) string
	Allowed() bool
	Owner(protocol int64, local string, localPort int64, remote string, remotePort int64) string
}

const (
	researchQueueRecords   = 256
	researchQueueBytes     = 1 << 20
	researchBatchBytes     = 60 << 10
	researchOwnerQueue     = 64
	researchPolicyInterval = 5 * time.Second
	researchFlowInterval   = 60 * time.Second
)

type researchExchange func(context.Context, gateway.ResearchBatch) (gateway.ResearchReply, error)
type researchQueued struct {
	record research.Record
	size   int
}
type researchOwnerJob struct {
	flow       *researchFlow
	generation uint64
}
type researchDNSJob struct {
	flow       *researchFlow
	generation uint64
	packet     []byte
	upstream   bool
	at         time.Time
}
type researchCollector struct {
	exit                                    research.ExitRef
	eligible                                func(*researchFlow) bool
	lookupUID                               FlowOwner
	mu                                      sync.Mutex
	observer                                ResearchObserver
	send                                    researchExchange
	ctx                                     context.Context
	cancel                                  context.CancelFunc
	requestCancel                           context.CancelFunc
	session                                 string
	generation                              uint64
	nextConsentSample, appliedConsentSample uint64
	consent, active, stopped                bool
	// An enabled attempt may reach the server even when its reply is canceled.
	// Keep a revocation pending until a current-generation disabled reply arrives.
	announced            bool
	nextFlow, nextRecord uint64
	flows                map[uint64]*researchFlow
	closedFlows          map[uint64]*researchFlow
	queue                []researchQueued
	queueBytes           int
	dropped              uint64
	owners               chan researchOwnerJob
	dns                  chan researchDNSJob
	wake                 chan struct{}
}
type researchFlow struct {
	tls                              *netobserve.ProtocolProbe
	exit                             research.ExitRef
	uid                              int64
	recording                        bool
	collector                        *researchCollector
	id                               uint64
	protocol                         int64
	local, remote                    string
	localPort, remotePort            int64
	app                              research.App
	from                             time.Time
	started                          *time.Time
	up, down, packetsUp, packetsDown uint64
	incomplete, closed               bool
	ownerPending                     bool
	closedAt                         time.Time
}

func newResearchCollector(o ResearchObserver, send researchExchange) *researchCollector {
	ctx, cancel := context.WithCancel(context.Background())
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		cancel()
		return nil
	}
	c := &researchCollector{observer: o, send: send, ctx: ctx, cancel: cancel, session: hex.EncodeToString(nonce[:]),
		generation: 1, flows: make(map[uint64]*researchFlow), closedFlows: make(map[uint64]*researchFlow), owners: make(chan researchOwnerJob, researchOwnerQueue),
		dns: make(chan researchDNSJob, researchOwnerQueue), wake: make(chan struct{}, 1)}
	go c.ownerLoop()
	return c
}

// SetResearch installs research only for the application's own tunnel protocols.
// Call after Start and before Attach. It has no effect on AWG, VLESS or direct routing.
func (g *Gateway) SetResearch(observer ResearchObserver) {
	g.mu.Lock()
	old := g.research
	g.research = nil
	if observer != nil && g.direct == nil && (g.cfg.Transport == "quic" || g.cfg.Transport == "https" || g.runtimeBond != nil) {
		g.research = newResearchCollector(observer, g.exchangeResearch)
		if g.research != nil {
			g.research.exit = g.researchExit
		}
	}
	current := g.research
	g.mu.Unlock()
	if old != nil {
		old.stop()
	}
	if current != nil {
		current.changed()
		go current.run()
	}
}

// ResearchChanged applies revocation synchronously; only the control exchange is async.
func (g *Gateway) ResearchChanged() {
	g.mu.Lock()
	c := g.research
	g.mu.Unlock()
	if c != nil {
		c.changed()
	}
}
func (g *Gateway) researchFlow(protocol int64, local string, localPort int64, remote string, remotePort int64) *researchFlow {
	g.mu.Lock()
	c := g.research
	g.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.openFlow(protocol, local, localPort, remote, remotePort)
}
func (g *Gateway) exchangeResearch(ctx context.Context, batch gateway.ResearchBatch) (gateway.ResearchReply, error) {
	return sendResearchBatch(ctx, g.open, batch)
}
func sendResearchBatch(ctx context.Context, open func(context.Context) (gateway.Stream, error), batch gateway.ResearchBatch) (gateway.ResearchReply, error) {
	var reply gateway.ResearchReply
	raw, err := json.Marshal(batch)
	if err != nil || len(raw) > 65535 {
		return reply, errors.New("research batch limit")
	}
	s, err := gateway.Open(ctx, open, "research-v1", "")
	if err != nil {
		return reply, err
	}
	defer s.Close()
	if deadline, ok := ctx.Deadline(); ok {
		s.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	if err = gateway.WritePacket(s, raw); err != nil {
		return reply, err
	}
	raw, err = gateway.ReadPacket(s, 65535)
	if err != nil {
		return reply, err
	}
	err = json.Unmarshal(raw, &reply)
	return reply, err
}
func (c *researchCollector) changed() {
	// Observer callbacks can overlap across the periodic poll and Android's
	// synchronous revoke notification. Never hold the flow mutex during JNI.
	c.mu.Lock()
	c.nextConsentSample++
	sample := c.nextConsentSample
	c.mu.Unlock()
	allowed := false
	if c.observer != nil {
		allowed = c.observer.Allowed()
	}
	c.mu.Lock()
	// A slow old grant cannot overwrite a newer applied revocation. Compare
	// applied tickets, not started tickets: a pending observer never stalls revoke.
	if sample <= c.appliedConsentSample {
		c.mu.Unlock()
		return
	}
	c.appliedConsentSample = sample
	changed := !c.stopped && allowed != c.consent
	if changed {
		c.consent = allowed
		c.disableLocked()
		c.generation++
		if c.requestCancel != nil {
			c.requestCancel()
		}
	}
	c.mu.Unlock()
	if changed {
		c.signal()
	}
}

// reset is also used before replacing an outer transport: re-negotiate current consent.
func (c *researchCollector) reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.disableLocked()
	c.generation++
	if c.requestCancel != nil {
		c.requestCancel()
	}
	c.mu.Unlock()
	c.signal()
}
func (c *researchCollector) stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.stopped = true
	c.disableLocked()
	c.cancel()
	if c.requestCancel != nil {
		c.requestCancel()
	}
	c.mu.Unlock()
}
func (c *researchCollector) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}
func (c *researchCollector) collecting() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active && !c.stopped
}
func (c *researchCollector) disableLocked() {
	// Direct history was collected lawfully. Freeze counters before ending the
	// observation interval; leave the bounded queue available for historical delivery.
	if c.exit.Route == "direct" && c.active && !c.stopped {
		eligible := c.eligible
		c.eligible = nil
		now := time.Now().UTC()
		for _, f := range c.flows {
			if f.recording {
				c.flowRecordLocked(f, "flow_delta", now)
			}
		}
		for _, f := range c.closedFlows {
			c.flowRecordLocked(f, "flow_close", f.closedAt)
		}
		c.eligible = eligible
	}
	c.active = false
	if c.exit.Route != "direct" || c.stopped {
		c.queue = nil
		c.queueBytes = 0
	}
	for _, f := range c.closedFlows {
		f.tls = nil
	}
	clear(c.closedFlows)
	for _, f := range c.flows {
		f.recording = false
		f.tls = nil
		f.up = 0
		f.down = 0
		f.packetsUp = 0
		f.packetsDown = 0
		f.from = time.Time{}
		f.started = nil
		f.app = unknownResearchApp()
		f.incomplete = true
	}
}
func (c *researchCollector) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var last time.Time
	for {
		force := false
		select {
		case <-c.ctx.Done():
			return
		case <-c.wake:
			force = true
		case <-ticker.C:
		}
		now := time.Now().UTC()
		c.flush(now)
		c.mu.Lock()
		queued := len(c.queue) > 0
		c.mu.Unlock()
		if force || queued || now.Sub(last) >= researchPolicyInterval {
			c.sync(now)
			last = now
		}
	}
}
func (c *researchCollector) sync(now time.Time) {
	c.changed()
	select {
	case <-c.wake:
	default:
	}
	c.mu.Lock()
	if c.stopped || (!c.consent && !c.announced && len(c.queue) == 0) {
		c.mu.Unlock()
		return
	}
	generation := c.generation
	batch := gateway.ResearchBatch{DirectEnabled: c.consent && c.exit.Route == "direct", Session: c.session, Generation: generation, Enabled: c.consent}
	if batch.Enabled {
		c.announced = true
	}
	if c.active || (c.exit.Route == "direct" && len(c.queue) > 0) {
		if c.dropped > 0 {
			n := c.dropped
			c.dropped = 0
			queued := len(c.queue)
			c.enqueueLocked("gap", 0, map[string]any{"dropped": strconv.FormatUint(n, 10), "reason": "client_queue_or_delivery_loss"}, now)
			if len(c.queue) == queued {
				c.dropped += n
			}
		}
		size := 256
		for len(c.queue) > 0 && len(batch.Records) < 32 {
			next := c.queue[0]
			if size+next.size > researchBatchBytes {
				break
			}
			batch.Records = append(batch.Records, next.record)
			if next.record.Generation != generation || !batch.Enabled {
				batch.Historical = true
			}
			size += next.size
			c.queueBytes -= next.size
			c.queue[0] = researchQueued{}
			c.queue = c.queue[1:]
		}
	}
	ctx, cancel := context.WithTimeout(c.ctx, 3*time.Second)
	c.requestCancel = cancel
	c.mu.Unlock()
	reply, err := c.send(ctx, batch)
	cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	// Never apply a late grant or retain an old generation's queued metadata.
	if c.stopped || generation != c.generation {
		return
	}
	c.requestCancel = nil
	if !batch.Enabled && err == nil {
		c.announced = false
	}
	if err != nil || !reply.Allowed || !c.consent || (c.exit.Route == "direct" && reply.DirectObservationVersion != 1) {
		lost := len(batch.Records) - max(0, reply.Accepted)
		if c.exit.Route != "direct" {
			lost += len(c.queue)
		}
		c.dropped += uint64(max(0, lost))
		c.disableLocked()
		return
	}
	if reply.Accepted < len(batch.Records) {
		c.dropped += uint64(len(batch.Records) - max(0, reply.Accepted))
	}
	if !c.active {
		c.active = true
		for _, f := range c.flows {
			c.beginLocked(f, now, true)
		}
	}
}
func (c *researchCollector) openFlow(protocol int64, local string, localPort int64, remote string, remotePort int64) *researchFlow {
	uid := int64(-1)
	if c.lookupUID != nil {
		uid = c.lookupUID.Owner(protocol, local, localPort, remote, remotePort)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || len(c.flows) >= gateway.MaxFlows {
		c.dropped++
		return nil
	}
	c.nextFlow++
	f := &researchFlow{exit: c.exit, uid: uid, collector: c, id: c.nextFlow, protocol: protocol, local: local, localPort: localPort, remote: remote, remotePort: remotePort, app: unknownResearchApp()}
	c.flows[f.id] = f
	if c.active {
		c.beginLocked(f, time.Now().UTC(), false)
	}
	return f
}
func (c *researchCollector) beginLocked(f *researchFlow, now time.Time, incomplete bool) {
	if c.eligible != nil && !c.eligible(f) {
		f.recording = false
		return
	}
	f.recording = true
	f.tls = netobserve.NewProtocolProbe(f.protocol, f.remotePort, incomplete)
	f.from = now
	f.incomplete = incomplete
	f.up = 0
	f.down = 0
	f.packetsUp = 0
	f.packetsDown = 0
	f.app = unknownResearchApp()
	if incomplete {
		f.started = nil
	} else {
		started := now
		f.started = &started
	}
	c.flowRecordLocked(f, "flow_open", now)
	select {
	case c.owners <- researchOwnerJob{f, c.generation}:
		f.ownerPending = true
	default:
		f.ownerPending = false
		c.dropped++
	}
}
func (f *researchFlow) add(up, down, packetsUp, packetsDown uint64) {
	if f == nil {
		return
	}
	c := f.collector
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active || c.stopped || f.closed || !f.recording || (c.eligible != nil && !c.eligible(f)) {
		return
	}
	f.up += up
	f.down += down
	f.packetsUp += packetsUp
	f.packetsDown += packetsDown
}
func (f *researchFlow) close() {
	if f == nil {
		return
	}
	c := f.collector
	c.mu.Lock()
	defer c.mu.Unlock()
	if f.closed {
		return
	}
	f.closed = true
	if c.active && !c.stopped && f.recording && (c.eligible == nil || c.eligible(f)) {
		f.closedAt = time.Now().UTC()
		if f.ownerPending && len(c.closedFlows) < gateway.MaxFlows {
			c.closedFlows[f.id] = f
		} else {
			c.flowRecordLocked(f, "flow_close", f.closedAt)
		}
	}
	delete(c.flows, f.id)
}
func (c *researchCollector) flush(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active || c.stopped {
		return
	}
	for id, f := range c.closedFlows {
		if now.Sub(f.closedAt) >= time.Second {
			c.flowRecordLocked(f, "flow_close", f.closedAt)
			delete(c.closedFlows, id)
		}
	}
	for _, f := range c.flows {
		if now.Sub(f.from) >= researchFlowInterval {
			c.flowRecordLocked(f, "flow_delta", now)
		}
	}
}
func (c *researchCollector) flowRecordLocked(f *researchFlow, kind string, now time.Time) {
	if !f.recording || (c.eligible != nil && !c.eligible(f)) {
		return
	}
	protocol := "tcp"
	if f.protocol == 17 {
		protocol = "udp"
	}
	d := research.FlowData{TLS: f.tls.Info(), Detected: f.tls.Detected(), ExitRef: f.exit, App: f.app, Protocol: protocol, Source: net.JoinHostPort(f.local, strconv.FormatInt(f.localPort, 10)),
		Destination: net.JoinHostPort(f.remote, strconv.FormatInt(f.remotePort, 10)), ObservedFrom: f.from, ObservedTo: now, StartedAt: f.started,
		BytesUp: f.up, BytesDown: f.down, PacketsUp: f.packetsUp, PacketsDown: f.packetsDown, CounterKind: "delta", MeasurementLevel: "logical_payload", Incomplete: f.incomplete}
	if kind == "flow_close" || f.closed {
		d.Status = "closed"
	} else {
		d.Status = "open"
	}
	c.enqueueLocked(kind, f.id, d, now)
	f.up = 0
	f.down = 0
	f.packetsUp = 0
	f.packetsDown = 0
	f.from = now
}
func (c *researchCollector) enqueueLocked(kind string, flow uint64, data any, now time.Time) {
	if !c.active || c.stopped {
		return
	}
	raw, err := json.Marshal(data)
	if err != nil {
		c.dropped++
		return
	}
	c.nextRecord++
	record := research.Record{Origin: "client", SchemaVersion: research.SchemaVersion, Type: kind, ID: c.session + "-" + strconv.FormatUint(c.nextRecord, 10),
		At: now.UTC(), Generation: c.generation, Flow: research.FlowKey{SessionID: c.session, FlowID: flow}, Data: raw}
	encoded, err := json.Marshal(record)
	if err != nil || len(encoded) > researchBatchBytes-256 || len(c.queue) >= researchQueueRecords || c.queueBytes+len(encoded) > researchQueueBytes {
		c.dropped++
		return
	}
	c.queue = append(c.queue, researchQueued{record, len(encoded)})
	c.queueBytes += len(encoded)
}
func unknownResearchApp() research.App {
	return research.App{UID: -1, Attribution: "unknown", Packages: []string{}, Labels: []string{}}
}
func parseResearchApp(raw string) research.App {
	app := unknownResearchApp()
	if len(raw) > 60<<10 || json.Unmarshal([]byte(raw), &app) != nil {
		return unknownResearchApp()
	}
	if app.Attribution != "known" && app.Attribution != "shared_uid" && app.Attribution != "unknown" {
		return unknownResearchApp()
	}
	if len(app.Packages) > 32 || len(app.Labels) > 32 {
		return unknownResearchApp()
	}
	for _, list := range [][]string{app.Packages, app.Labels} {
		for _, name := range list {
			if len(name) > 1024 {
				return unknownResearchApp()
			}
		}
	}
	return app
}
func (c *researchCollector) ownerLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case job := <-c.owners:
			c.mu.Lock()
			valid := c.active && !c.stopped && c.generation == job.generation && job.flow.recording && (c.eligible == nil || c.eligible(job.flow))
			c.mu.Unlock()
			if !valid {
				continue
			}
			f := job.flow
			var raw string
			if f.uid >= 0 {
				raw = c.observer.OwnerForUID(f.uid)
			} else {
				raw = c.observer.Owner(f.protocol, f.local, f.localPort, f.remote, f.remotePort)
			}
			app := parseResearchApp(raw)
			c.mu.Lock()
			if c.active && !c.stopped && c.generation == job.generation && job.flow.recording && (c.eligible == nil || c.eligible(job.flow)) {
				f.app = app
				f.ownerPending = false
				now := time.Now().UTC()
				c.enqueueLocked("app", f.id, struct {
					research.App
					research.ExitRef
				}{app, f.exit}, now)
				if c.closedFlows[f.id] == f {
					c.flowRecordLocked(f, "flow_close", f.closedAt)
					delete(c.closedFlows, f.id)
				}
			}
			c.mu.Unlock()
		case job := <-c.dns:
			c.recordDNS(job)
		}
	}
}

// Correlation uses opaque IDs only; the research control stream is the sole grant.
// Keeping the ID on dormant flows allows opt-in without breaking a VPN connection.
func (f *researchFlow) dialContext(ctx context.Context) context.Context {
	if f == nil {
		return ctx
	}
	c := f.collector
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || f.closed {
		return ctx
	}
	return gateway.WithResearchFlow(ctx, gateway.ResearchFlowRef{Version: 1, Session: c.session, FlowID: f.id})
}
