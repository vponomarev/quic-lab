package gateway

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net"
	"quiclab/internal/netobserve"
	"quiclab/internal/research"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ResearchFlowRef is correlation metadata, never consent or an authenticated identity.
// The server assigns its own flow ID and keeps this client claim separately.
type ResearchFlowRef struct {
	Version int    `json:"version"`
	Session string `json:"session"`
	FlowID  uint64 `json:"flow_id,string"`
}

func (r ResearchFlowRef) valid() bool {
	if r.Version != 1 || len(r.Session) != 32 || r.FlowID == 0 {
		return false
	}
	_, e := hex.DecodeString(r.Session)
	return e == nil
}

type researchFlowContextKey struct{}

func WithResearchFlow(ctx context.Context, ref ResearchFlowRef) context.Context {
	return context.WithValue(ctx, researchFlowContextKey{}, ref)
}
func researchFlowFromContext(ctx context.Context) *ResearchFlowRef {
	ref, ok := ctx.Value(researchFlowContextKey{}).(ResearchFlowRef)
	if !ok || !ref.valid() {
		return nil
	}
	return &ref
}
func identityNonce() string { return identity() }

type observedDNS struct {
	packet []byte
	up     bool
	at     time.Time
}
type observedConnection struct {
	statsMu         sync.Mutex
	tls             *netobserve.ProtocolProbe
	upstreamSeen    atomic.Bool
	ready           atomic.Bool
	resolveIdentity bool
	ioMu            sync.Mutex
	ioWG            sync.WaitGroup
	ioClosed        bool
	net.Conn
	receiver                           *ResearchReceiver
	cs                                 tls.ConnectionState
	user, device, network, destination string
	ref                                ResearchFlowRef
	id                                 uint64
	generation                         uint64
	from                               time.Time
	up, down, packetsUp, packetsDown   uint64
	dns                                []observedDNS
	dropped                            atomic.Uint64
	stop                               chan struct{}
	once                               sync.Once
}

func (s *Server) observeConnection(ctx context.Context, req Request, network string, conn net.Conn) net.Conn {
	r := s.Research
	if r == nil || req.Research == nil || !req.Research.valid() || r.identity == nil || r.appendRecord == nil {
		return conn
	}
	cs, ok := ctx.Value(researchContextKey{}).(tls.ConnectionState)
	if !ok {
		return conn
	}
	// Diagnostic resource pressure must never prevent a data connection.
	if !r.mu.TryLock() {
		return conn
	}
	if r.observers >= MaxFlows {
		r.mu.Unlock()
		return conn
	}
	snapshot := r.identities[observationIdentityKey(cs)]
	r.observers++
	r.nextObservedFlow++
	f := &observedConnection{Conn: conn, receiver: r, cs: cs, resolveIdentity: snapshot.device == "", user: snapshot.user, device: snapshot.device, network: network, destination: req.Address, ref: *req.Research, id: r.nextObservedFlow, stop: make(chan struct{})}
	if state, ok := f.stateLocked(time.Now()); ok {
		f.generation = state.generation
	}
	r.connections[f] = struct{}{}
	f.ready.Store(!f.resolveIdentity)
	r.mu.Unlock()
	go f.run()
	return f
}
func (f *observedConnection) Read(b []byte) (int, error) {
	if !f.beginIO() {
		return 0, net.ErrClosed
	}
	defer f.ioWG.Done()
	n, e := f.Conn.Read(b)
	if n > 0 {
		f.payload(b[:n], false)
	}
	return n, e
}
func (f *observedConnection) Write(b []byte) (int, error) {
	if !f.beginIO() {
		return 0, net.ErrClosed
	}
	defer f.ioWG.Done()
	n, e := f.Conn.Write(b)
	if n > 0 {
		f.payload(b[:n], true)
	}
	return n, e
}
func (f *observedConnection) Close() error {
	f.ioMu.Lock()
	f.ioClosed = true
	f.ioMu.Unlock()
	e := f.Conn.Close()
	f.once.Do(func() { close(f.stop) })
	return e
}

// Preserve half-close when Relay probes the wrapped connection.
func (f *observedConnection) CloseWrite() error {
	if c, ok := f.Conn.(interface{ CloseWrite() error }); ok {
		return c.CloseWrite()
	}
	return f.Close()
}
func (f *observedConnection) reset() {
	f.up = 0
	f.down = 0
	f.packetsUp = 0
	f.packetsDown = 0
	f.dns = nil
	f.tls = nil
	f.from = time.Time{}
	f.dropped.Store(0)
}
func (f *observedConnection) stateLocked(now time.Time) (researchSession, bool) {
	value, ok := f.receiver.observationStates.Load(f.device + "/" + f.ref.Session)
	if !ok {
		return researchSession{}, false
	}
	state := value.(researchSession)
	return state, state.enabled && state.allowed && now.Before(state.until)
}
func (f *observedConnection) payload(b []byte, up bool) {
	firstUp := false
	if up {
		firstUp = !f.upstreamSeen.Swap(true)
	}
	if !f.ready.Load() {
		return
	}
	// This lock protects counters and bounded DNS/TLS prefix inspection.
	// No policy lookup, JSON parsing, persistence or receiver lock happens here.
	f.statsMu.Lock()
	defer f.statsMu.Unlock()
	now := time.Now().UTC()
	state, ok := f.stateLocked(now)
	if !ok {
		f.reset()
		return
	}
	if f.generation != state.generation {
		f.reset()
		f.generation = state.generation
	}
	if f.from.IsZero() {
		f.from = now
	}
	if up {
		f.up += uint64(len(b))
	} else {
		f.down += uint64(len(b))
	}
	if up {
		if f.tls == nil {
			proto := int64(6)
			if f.network == "udp4" {
				proto = 17
			}
			_, port, _ := net.SplitHostPort(f.destination)
			number, _ := strconv.ParseInt(port, 10, 64)
			f.tls = netobserve.NewProtocolProbe(proto, number, !firstUp)
		}
		f.tls.Feed(b)
	}
	if f.network != "udp4" {
		return
	}
	if up {
		f.packetsUp++
	} else {
		f.packetsDown++
	}
	_, port, _ := net.SplitHostPort(f.destination)
	if port == "53" && len(b) >= 12 && len(b) <= 4096 {
		if len(f.dns) >= 16 {
			f.dropped.Add(1)
			return
		}
		f.dns = append(f.dns, observedDNS{append([]byte(nil), b...), up, now})
	}
}
func (f *observedConnection) run() {
	// Resolve uncached identity off the forwarding path. Until resolved no data
	// is collected. An established control session normally supplies the cache.
	r := f.receiver
	if f.resolveIdentity {
		user, device, _ := r.identity(f.cs)
		r.mu.Lock()
		f.user = user
		f.device = device
		if state, ok := f.stateLocked(time.Now()); ok {
			f.generation = state.generation
		}
		f.ready.Store(user != "" && device != "")
		r.mu.Unlock()
	}

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	defer func() {
		f.receiver.mu.Lock()
		delete(f.receiver.connections, f)
		f.receiver.observers--
		f.receiver.mu.Unlock()
	}()
	for {
		select {
		case <-ticker.C:
			f.flush(false)
		case <-f.stop:
			f.ioWG.Wait()
			f.flush(true)
			return
		}
	}
}
func (f *observedConnection) flush(closed bool) {
	r := f.receiver
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	f.statsMu.Lock()
	if state, ok := f.stateLocked(now); !ok || state.generation != f.generation {
		f.reset()
	}
	generation, from := f.generation, f.from
	up, down, pu, pd := f.up, f.down, f.packetsUp, f.packetsDown
	tlsInfo := f.tls.Info()
	detected := f.tls.Detected()
	jobs := f.dns
	lost := f.dropped.Swap(0)
	f.up = 0
	f.down = 0
	f.packetsUp = 0
	f.packetsDown = 0
	f.dns = nil
	f.from = time.Time{}
	f.statsMu.Unlock()
	state, ok := f.stateLocked(now)
	user, device, allowed := r.identity(f.cs)
	if !ok || !allowed || user != f.user || device != f.device || generation != state.generation {
		return
	}
	ref := research.FlowKey{DeviceID: f.device, SessionID: f.ref.Session, FlowID: f.ref.FlowID}
	emit := func(kind string, data any, at time.Time) {
		if !f.emitLocked(kind, data, at, generation) {
			lost++
		}
	}
	if !from.IsZero() {
		proto := "tcp"
		if f.network == "udp4" {
			proto = "udp"
		}
		data := research.FlowData{TLS: tlsInfo, Detected: detected, ExitRef: research.ExitRef{Route: "vpn", Type: "unknown"}, UserID: f.user, App: research.App{Attribution: "unknown"}, Protocol: proto, Destination: f.destination, ClientFlow: &ref, ObservedFrom: from, ObservedTo: now, BytesUp: up, BytesDown: down, PacketsUp: pu, PacketsDown: pd, CounterKind: "delta", MeasurementLevel: "exit_socket_payload", Incomplete: true, Reason: "observation_window_not_full_connection"}
		if app, ok := r.apps[appKey(ref)]; ok && app.generation == generation && now.Sub(app.at) <= 2*time.Minute && app.protocol == proto && app.destination == f.destination {
			data.App = app.app
			data.AppSource = "client"
			if app.exit.Route == "vpn" {
				data.ExitRef = app.exit
			}
		}
		kind := "flow_delta"
		if closed {
			kind = "flow_close"
		}
		emit(kind, data, now)
		for _, job := range jobs {
			m, e := netobserve.DecodeDNS(job.packet)
			if e != nil {
				continue
			}
			fields := m.Fields()
			fields["user_id"] = f.user
			fields["route"] = data.Route
			fields["exit_id"] = data.ID
			fields["exit_type"] = data.Type
			fields["app"] = data.App
			fields["app_source"] = data.AppSource
			fields["protocol"] = "udp"
			fields["destination"] = f.destination
			fields["client_flow"] = ref
			fields["direction"] = "response"
			if job.up {
				fields["direction"] = "query"
			}
			emit("dns", fields, job.at)
		}
	}
	if lost > 0 {
		if !f.emitLocked("gap", map[string]any{"user_id": f.user, "dropped": strconv.FormatUint(lost, 10), "reason": "server_observer_queue_or_delivery_loss", "client_flow": ref}, now, generation) {
			f.statsMu.Lock()
			if f.generation == generation {
				f.dropped.Add(lost + 1)
			}
			f.statsMu.Unlock()
		}
	}
}
func (f *observedConnection) emitLocked(kind string, data any, at time.Time, generation uint64) bool {
	raw, e := json.Marshal(data)
	if e != nil {
		return false
	}
	r := f.receiver
	r.nextObservedRecord++
	record := research.Record{SchemaVersion: research.SchemaVersion, Origin: "server_gateway", ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano), At: at, Generation: generation, Type: kind, ID: r.epoch + "-" + strconv.FormatUint(r.nextObservedRecord, 10), Flow: research.FlowKey{DeviceID: f.device, SessionID: r.epoch, FlowID: f.id}, Data: raw}
	return r.appendRecord(record)
}

type observedApp struct {
	exit                  research.ExitRef
	generation            uint64
	at                    time.Time
	app                   research.App
	protocol, destination string
}

func appKey(k research.FlowKey) string {
	return k.DeviceID + "/" + k.SessionID + "/" + strconv.FormatUint(k.FlowID, 10)
}
func (r *ResearchReceiver) rememberAppLocked(rec research.Record) {
	if rec.Type != "app" && rec.Type != "flow_open" && rec.Type != "flow_delta" && rec.Type != "flow_close" {
		return
	}
	key := appKey(rec.Flow)
	now := time.Now()
	previous, exists := r.apps[key]
	if previous.generation != rec.Generation || now.Sub(previous.at) > 2*time.Minute {
		previous = observedApp{}
	}
	var d research.FlowData
	if rec.Type == "app" {
		if json.Unmarshal(rec.Data, &d.App) != nil {
			return
		}
		d.ExitRef = previous.exit
		d.Protocol = previous.protocol
		d.Destination = previous.destination
	} else {
		if json.Unmarshal(rec.Data, &d) != nil {
			return
		}
		if d.App.Attribution == "unknown" && previous.app.Attribution != "" {
			d.App = previous.app
		}
	}
	raw, _ := json.Marshal(d.App)
	if len(raw) > 4096 {
		return
	}
	if !exists && len(r.apps) >= 1024 {
		for k, v := range r.apps {
			if now.Sub(v.at) > 2*time.Minute {
				delete(r.apps, k)
			}
		}
		if len(r.apps) >= 1024 {
			return
		}
	}
	r.apps[key] = observedApp{d.ExitRef, rec.Generation, now, d.App, d.Protocol, d.Destination}
}

type observedIdentity struct{ user, device string }

func observationIdentityKey(cs tls.ConnectionState) [32]byte {
	if len(cs.PeerCertificates) == 0 {
		return [32]byte{}
	}
	return sha256.Sum256(cs.PeerCertificates[0].Raw)
}

func (f *observedConnection) beginIO() bool {
	f.ioMu.Lock()
	defer f.ioMu.Unlock()
	if f.ioClosed {
		return false
	}
	f.ioWG.Add(1)
	return true
}
