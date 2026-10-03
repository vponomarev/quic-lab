// Package bond joins authenticated packet paths into a bounded, resumable session.
// Paths must encrypt/authenticate packets and enforce congestion control themselves.
package bond

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sort"
	"sync"
	"time"
)

const (
	Data       byte = 1
	Open       byte = 2
	Fin        byte = 3
	Reset      byte = 4
	Datagram   byte = 5
	ack        byte = 6
	ping       byte = 7
	pong       byte = 8
	Stop       byte = 9
	blockCell  byte = 10
	terminate  byte = 11
	terminated byte = 12
	Chunk           = 1050
	header          = 30
	maxPending      = 1024
)

type Path interface {
	SendDatagram([]byte) error
	ReceiveDatagram(context.Context) ([]byte, error)
	Close() error
}
type Record struct {
	Kind    byte
	Flow    uint32
	Seq     uint64
	Payload []byte
}
type packet struct {
	Record
	id            uint64
	created, last time.Time
	primary       string
	copied        bool
	attempts      int
}
type traffic struct{ sent, received, copies, probes, rxCopies, rxControl uint64 }
type pathState struct {
	ackedBytes, dataProbes uint64
	dataProbeStamp         int64
	dataProbeSent          time.Time
	info                   PathInfo
	*traffic
	p                                                 Path
	rtt, variance                                     time.Duration
	lastReply, lastSend, healthySince, penalizedUntil time.Time
	failures                                          int
}
type PathStats struct {
	Draining         bool    `json:"draining,omitempty"`
	ProfileID        string  `json:"profile_id"`
	Network          string  `json:"network"`
	Generation       uint64  `json:"generation"`
	AckedBytes       uint64  `json:"acked_bytes"`
	PendingBytes     uint64  `json:"pending_bytes"`
	DataProbes       uint64  `json:"data_probes"`
	DataProbePending bool    `json:"data_probe_pending"`
	RXCopies         uint64  `json:"rx_copies"`
	RXControl        uint64  `json:"rx_control"`
	Name             string  `json:"name"`
	RTTMS            float64 `json:"rtt_ms"`
	Ready            bool    `json:"ready"`
	Sent             uint64  `json:"sent"`
	Received         uint64  `json:"received"`
	Copies           uint64  `json:"copies"`
	Probes           uint64  `json:"probes"`
}
type Stats struct {
	Paths       []PathStats `json:"paths"`
	Pending     int         `json:"pending"`
	OldestMS    int64       `json:"oldest_ms"`
	Duplicates  uint64      `json:"duplicates"`
	Rescued     uint64      `json:"rescued"`
	Expired     uint64      `json:"expired"`
	CellSent    uint64      `json:"cell_sent"`
	CellBlocked bool        `json:"cell_blocked"`
}
type Session struct {
	preferredPath    string
	terminateAck     chan struct{}
	terminateHandler func()
	options          Options
	generations      map[string]uint64
	cellBlocked      bool
	totals           map[string]*traffic
	recordTimeout    time.Duration
	mu               sync.Mutex
	ctx              context.Context
	cancel           context.CancelFunc
	paths            map[string]*pathState
	pending          map[uint64]*packet
	perFlow          map[uint32]int
	next             uint64
	changed          chan struct{}
	deliver          func(Record) bool
	// Datagram duplicate window never stalls a reliable stream.
	seen                                     [65536]uint64
	seenHigh                                 uint64
	duplicate, rescued, expired              uint64
	emptySince                               time.Time
	configured                               bool
	copyLimit, cellLimit, cellSent, copyUsed uint64
	budgetStart                              time.Time
	lastTrial                                time.Time
}

func New(ctx context.Context, deliver func(Record) bool) *Session {
	return NewWithOptions(ctx, deliver, Options{})
}
func (s *Session) Configure(copyPerMinute, cellPerDirection uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configured = true
	s.copyLimit = copyPerMinute
	s.cellLimit = cellPerDirection
	s.budgetStart = time.Now()
}
func (s *Session) CellAllowed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.cellBlocked && (s.cellLimit == 0 || s.cellSent < s.cellLimit)
}
func (s *Session) BlockCell() { s.mu.Lock(); s.cellBlocked = true; s.mu.Unlock() }

// BlockCellAndNotify sends a best-effort hint only over non-cellular paths.
// The socket meter is authoritative even if this hint is lost or unsupported.
func (s *Session) BlockCellAndNotify() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cellBlocked {
		return
	}
	s.cellBlocked = true
	for name, p := range s.paths {
		if p.info.Network != "cell" {
			s.sendOn(name, p, frame(blockCell, 0, 0, 0, 0, false, nil), true)
		}
	}
}
func (s *Session) noteDuplicate() { s.mu.Lock(); s.duplicate++; s.mu.Unlock() }
func (s *Session) sendOn(name string, p *pathState, b []byte, control bool) error {
	if p.info.Network == "cell" && (s.cellBlocked || s.cellLimit > 0 && s.cellSent+uint64(len(b)) > s.cellLimit) {
		s.cellBlocked = true
		return errors.New("LTE session budget exhausted")
	}
	if e := p.p.SendDatagram(b); e != nil {
		return e
	}
	p.sent += uint64(len(b))
	p.lastSend = time.Now()
	if p.info.Network == "cell" {
		s.cellSent += uint64(len(b))
	}
	if control {
		p.probes += uint64(len(b))
	}
	return nil
}
func (s *Session) Context() context.Context { return s.ctx }
func (s *Session) signal()                  { close(s.changed); s.changed = make(chan struct{}) }
func (s *Session) AddPath(name string, p Path) error {
	return s.addPath(PathInfo{ID: name, ProfileID: name, Network: name}, p)
}
func (s *Session) addPath(info PathInfo, p Path) error {
	name := info.ID
	if p == nil || name == "" || len(name) > 128 || len(info.ProfileID) > 128 || (info.Network != "wifi" && info.Network != "cell") {
		return errors.New("invalid path")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return net.ErrClosed
	}
	if info.Network == "cell" && (s.cellBlocked || s.cellLimit > 0 && s.cellSent >= s.cellLimit) {
		return errors.New("LTE session budget exhausted")
	}
	if info.Generation == 0 {
		info.Generation = s.generations[name] + 1
	}
	if info.Generation <= s.generations[name] {
		return errors.New("stale path generation")
	}
	if _, known := s.generations[name]; !known && len(s.generations) >= 1024 {
		return errors.New("path identity limit")
	}
	s.generations[name] = info.Generation
	if old := s.paths[name]; old != nil {
		old.p.Close()
	}
	now := time.Now()
	if len(s.paths) == 0 && !s.emptySince.IsZero() && now.Sub(s.emptySince) >= s.options.DisconnectGrace {
		s.cancel()
		return net.ErrClosed
	}
	healthy := now
	// A first Wi-Fi path may be used immediately; a recovered path must prove stability.
	if s.next == 0 && len(s.paths) == 0 {
		healthy = now.Add(-10 * time.Second)
	}
	if s.totals[name] == nil {
		s.totals[name] = &traffic{}
	}
	ps := &pathState{info: info, traffic: s.totals[name], p: p, rtt: 100 * time.Millisecond, variance: 25 * time.Millisecond, lastReply: now, healthySince: healthy}
	if len(s.paths) == 0 && !s.emptySince.IsZero() {
		for _, packet := range s.pending {
			if packet.Kind != Datagram {
				packet.created = packet.created.Add(now.Sub(maxTime(s.emptySince, packet.created)))
			}
		}
	}
	s.paths[name] = ps
	s.emptySince = time.Time{}
	s.signal()
	go s.receive(name, ps)
	return nil
}
func (s *Session) RemovePath(name string) { s.removePath(name, 0) }
func (s *Session) RemoveNamedPath(info PathInfo) {
	if info.Generation != 0 {
		s.removePath(info.ID, info.Generation)
	}
}
func (s *Session) removePath(name string, generation uint64) {
	s.mu.Lock()
	if p := s.paths[name]; p != nil && (generation == 0 || p.info.Generation == generation) {
		delete(s.paths, name)
		if len(s.paths) == 0 && s.emptySince.IsZero() {
			s.emptySince = time.Now()
		}
		p.p.Close()
		s.signal()
	}
	s.mu.Unlock()
}
func (s *Session) HasPath(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paths[name] != nil
}
func (s *Session) Close() error {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	for n, p := range s.paths {
		p.p.Close()
		delete(s.paths, n)
	}
	s.signal()
	return nil
}
func (s *Session) Send(ctx context.Context, r Record) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if len(r.Payload) > Chunk {
		return errors.New("bond record too large")
	}
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		s.mu.Lock()
		if s.ctx.Err() != nil {
			s.mu.Unlock()
			return net.ErrClosed
		}
		if len(s.pending) < s.options.MaxPendingSession && s.perFlow[r.Flow] < s.options.MaxPendingFlow {
			s.next++
			p := &packet{Record: r, id: s.next, created: time.Now()}
			p.Payload = append([]byte(nil), r.Payload...)
			s.pending[p.id] = p
			s.perFlow[r.Flow]++
			s.transmit(p, "", false)
			s.mu.Unlock()
			return nil
		}
		if r.Kind == Datagram {
			s.expired++
			s.mu.Unlock()
			return nil
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return net.ErrClosed
		case <-changed:
		}
	}
}
func frame(kind byte, flow uint32, seq, id uint64, stamp int64, copy bool, payload []byte) []byte {
	b := make([]byte, header+len(payload))
	b[0] = kind
	if copy {
		b[1] = 1
	}
	binary.BigEndian.PutUint32(b[2:], flow)
	binary.BigEndian.PutUint64(b[6:], seq)
	binary.BigEndian.PutUint64(b[14:], id)
	binary.BigEndian.PutUint64(b[22:], uint64(stamp))
	copyBytes(b[header:], payload)
	return b
}
func copyBytes(dst, src []byte) { copy(dst, src) }
func (s *Session) preferred(now time.Time) string {
	if p := s.paths[s.preferredPath]; p != nil && now.After(p.penalizedUntil) && now.Sub(p.lastReply) < 2*time.Second && (p.info.Network != "cell" || !s.cellBlocked && (s.cellLimit == 0 || s.cellSent < s.cellLimit)) {
		return s.preferredPath
	}
	wn, cn := "", ""
	for name, p := range s.paths {
		if p.info.Network == "wifi" && (wn == "" || p.rtt < s.paths[wn].rtt || p.rtt == s.paths[wn].rtt && name < wn) {
			wn = name
		}
		if p.info.Network == "cell" && (cn == "" || p.rtt < s.paths[cn].rtt || p.rtt == s.paths[cn].rtt && name < cn) {
			cn = name
		}
	}
	w, c := s.paths[wn], s.paths[cn]
	if s.cellBlocked || s.cellLimit > 0 && s.cellSent >= s.cellLimit {
		c = nil
	}
	if w != nil {
		if c == nil {
			return wn
		}
		if now.Sub(c.lastReply) > 2*time.Second && now.Sub(w.lastReply) < 2*time.Second {
			return wn
		}
		healthy := now.Sub(w.healthySince)
		if now.After(w.penalizedUntil) && now.Sub(w.lastReply) < 2*time.Second && healthy > 5*time.Second && w.rtt <= max(150*time.Millisecond, c.rtt*3/2) {
			if healthy > 8*time.Second {
				return wn
			}
			if now.Sub(s.lastTrial) > 100*time.Millisecond {
				s.lastTrial = now
				return wn
			}
		}
	}
	if c != nil {
		return cn
	}
	return ""
}
func (s *Session) transmit(p *packet, via string, duplicate bool) {
	now := time.Now()
	speculative := duplicate && via != ""
	if via == "" {
		via = s.preferred(now)
	}
	ps := s.paths[via]
	if ps == nil {
		return
	}
	b := frame(p.Kind, p.Flow, p.Seq, p.id, now.UnixNano(), duplicate, p.Payload)
	if now.Sub(s.budgetStart) >= time.Minute {
		s.copyUsed = 0
		s.budgetStart = now
	}
	if speculative && s.configured && s.copyUsed+uint64(len(b)) > s.copyLimit {
		return
	}
	if e := s.sendOn(via, ps, b, false); e != nil {
		return
	}
	if p.primary == "" {
		p.primary = via
	}
	p.last = now
	p.attempts++
	p.copied = p.copied || duplicate
	ps.lastSend = now
	if speculative {
		s.copyUsed += uint64(len(b))
	}
	if duplicate {
		ps.copies += uint64(len(b))
	}
}
func hedge(p *pathState) time.Duration {
	if p == nil {
		return 200 * time.Millisecond
	}
	d := p.rtt + 4*p.variance + 10*time.Millisecond
	if d < 60*time.Millisecond {
		d = 60 * time.Millisecond
	}
	if d > 500*time.Millisecond {
		d = 500 * time.Millisecond
	}
	return d
}
func (s *Session) remove(id uint64, p *packet) {
	delete(s.pending, id)
	s.perFlow[p.Flow]--
	if s.perFlow[p.Flow] == 0 {
		delete(s.perFlow, p.Flow)
	}
	s.signal()
}
func (s *Session) run() {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	defer s.Close()
	for {
		select {
		case <-s.ctx.Done():
			return
		case now := <-t.C:
			expiredFlows := map[uint32]bool{}
			s.mu.Lock()
			if len(s.paths) == 0 {
				if s.emptySince.IsZero() {
					s.emptySince = now
				}
				if now.Sub(s.emptySince) >= s.options.DisconnectGrace {
					s.cancel()
				}
			} else {
				s.emptySince = time.Time{}
			}
			for name, p := range s.paths {
				if p.info.Network == "cell" && s.cellBlocked {
					delete(s.paths, name)
					if len(s.paths) == 0 && s.emptySince.IsZero() {
						s.emptySince = time.Now()
					}
					p.p.Close()
					continue
				}
				if now.Sub(p.lastSend) > time.Second || now.Sub(p.lastReply) > time.Second && now.Sub(p.lastSend) > 500*time.Millisecond {
					b := frame(ping, 0, 0, 0, now.UnixNano(), false, nil)
					if s.sendOn(name, p, b, true) == nil {
						p.lastSend = now
					}
				}
				if now.Sub(p.healthySince) > 30*time.Second {
					p.failures = 0
				}
				if now.Sub(p.lastReply) > 2*time.Second {
					p.healthySince = now
				}
				_ = name
			}
			for id, p := range s.pending {
				age := now.Sub(p.created)
				if p.Kind == Datagram && age > 200*time.Millisecond {
					s.expired++
					s.remove(id, p)
					continue
				}
				if len(s.paths) == 0 && p.Kind != Datagram {
					continue
				}
				if age > s.recordTimeout {
					if p.Kind == Reset {
						s.remove(id, p)
					} else {
						expiredFlows[p.Flow] = true
					}
					continue
				}

				if p.last.IsZero() {
					s.transmit(p, "", false)
					continue
				}
				threshold := hedge(s.paths[p.primary])
				// A speculative UDP rescue must run before its 200ms lifetime.
				// Leave half the lifetime for the alternate path and scheduler jitter.
				if p.Kind == Datagram {
					threshold = min(threshold, 100*time.Millisecond)
				}
				if !p.copied && now.Sub(p.last) > threshold {
					other := ""
					for name, path := range s.paths {
						if name != p.primary && (other == "" || path.rtt < s.paths[other].rtt || path.rtt == s.paths[other].rtt && name < other) {
							other = name
						}
					}
					if s.paths[other] != nil {
						if bad := s.paths[p.primary]; bad != nil {
							bad.healthySince = now
							if now.After(bad.penalizedUntil) {
								bad.failures = min(bad.failures+1, 4)
							}
							bad.penalizedUntil = now.Add(time.Duration(1<<bad.failures) * time.Second)
						}
						s.transmit(p, other, true)
					}
				}
				if p.Kind != Datagram && now.Sub(p.last) > max(2*threshold, 200*time.Millisecond) {
					s.transmit(p, "", p.attempts > 0)
				}
			}
			for id, p := range s.pending {
				if expiredFlows[p.Flow] {
					s.remove(id, p)
				}
			}
			s.mu.Unlock()
			for flow := range expiredFlows {
				s.deliver(Record{Kind: Reset, Flow: flow})
				ctx, cancel := context.WithTimeout(s.ctx, time.Second)
				s.Send(ctx, Record{Kind: Reset, Flow: flow})
				cancel()
			}
		}
	}
}
func (s *Session) receive(name string, ps *pathState) {
	defer func() {
		s.mu.Lock()
		if s.paths[name] == ps {
			delete(s.paths, name)
			if len(s.paths) == 0 && s.emptySince.IsZero() {
				s.emptySince = time.Now()
			}
			s.signal()
		}
		s.mu.Unlock()
		ps.p.Close()
	}()
	for {
		b, e := ps.p.ReceiveDatagram(s.ctx)
		if e != nil {
			return
		}
		if len(b) < header || len(b) > header+Chunk {
			continue
		}
		kind := b[0]
		id := binary.BigEndian.Uint64(b[14:])
		stamp := int64(binary.BigEndian.Uint64(b[22:]))
		now := time.Now()
		s.mu.Lock()
		if s.paths[name] != ps {
			s.mu.Unlock()
			return
		}
		ps.received += uint64(len(b))
		if kind >= ack {
			ps.rxControl += uint64(len(b))
		} else if b[1] == 1 {
			ps.rxCopies += uint64(len(b))
		}
		if kind == terminated && len(b) == header {
			if s.terminateAck != nil {
				close(s.terminateAck)
				s.terminateAck = nil
			}
			s.mu.Unlock()
			continue
		}
		if kind == terminate && len(b) == header {
			handler := s.terminateHandler
			s.mu.Unlock()
			// Only a server with an explicit registry owner accepts termination. Paths
			// are authenticated; tokens never travel in the control record or logs.
			if handler != nil {
				handler()
				s.mu.Lock()
				_ = s.sendOn(name, ps, frame(terminated, 0, 0, 0, 0, false, nil), true)
				s.mu.Unlock()
				s.Close()
				return
			}
			continue
		}
		if kind == blockCell && len(b) == header {
			s.cellBlocked = true
			s.mu.Unlock()
			continue
		}
		if kind == ack || kind == pong {
			elapsed := now.Sub(time.Unix(0, stamp))
			if elapsed > 0 && elapsed < 10*time.Second {
				delta := elapsed - ps.rtt
				if delta < 0 {
					delta = -delta
				}
				ps.variance = (3*ps.variance + delta) / 4
				ps.rtt = (7*ps.rtt + elapsed) / 8
			}
			ps.lastReply = now
			if kind == pong && len(b) == header+Chunk && stamp == ps.dataProbeStamp {
				ps.dataProbes++
				ps.dataProbeStamp = 0
				ps.dataProbeSent = time.Time{}
			}
			if p := s.pending[id]; kind == ack && p != nil {
				if p.Kind == Data || p.Kind == Datagram {
					ps.ackedBytes += uint64(len(p.Payload))
				}
				if name != p.primary {
					s.rescued++
				}
				s.remove(id, p)
			}
			s.mu.Unlock()
			continue
		}
		s.mu.Unlock()
		if kind == ping {
			reply := frame(pong, 0, 0, 0, stamp, false, b[header:])
			s.mu.Lock()
			s.sendOn(name, ps, reply, true)
			s.mu.Unlock()
			continue
		}
		if kind < Data || (kind > Datagram && kind != Stop) {
			continue
		}
		r := Record{Kind: kind, Flow: binary.BigEndian.Uint32(b[2:]), Seq: binary.BigEndian.Uint64(b[6:]), Payload: append([]byte(nil), b[header:]...)}
		duplicate := false
		if kind == Datagram {
			s.mu.Lock()
			duplicate = s.seen[id%65536] == id || (s.seenHigh > 65536 && id <= s.seenHigh-65536)
			if !duplicate {
				s.seen[id%65536] = id
				if id > s.seenHigh {
					s.seenHigh = id
				}
			}

			if duplicate {
				s.duplicate++
			}
			s.mu.Unlock()
		}
		accepted := duplicate || s.deliver(r)
		if accepted {
			s.mu.Lock()
			s.sendOn(name, ps, frame(ack, r.Flow, r.Seq, id, stamp, false, nil), true)
			s.mu.Unlock()
		}
	}
}
func (s *Session) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	v := Stats{CellSent: s.cellSent, CellBlocked: s.cellBlocked || s.cellLimit > 0 && s.cellSent >= s.cellLimit, Pending: len(s.pending), Duplicates: s.duplicate, Rescued: s.rescued, Expired: s.expired}
	names := make([]string, 0, len(s.totals))
	for n := range s.totals {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		total := s.totals[n]
		if total == nil {
			continue
		}
		vpath := PathStats{Name: n, Sent: total.sent, Received: total.received, Copies: total.copies, Probes: total.probes, RXCopies: total.rxCopies, RXControl: total.rxControl}
		if p := s.paths[n]; p != nil {
			vpath.ProfileID, vpath.Network, vpath.Generation = p.info.ProfileID, p.info.Network, p.info.Generation
			vpath.AckedBytes, vpath.DataProbes = p.ackedBytes, p.dataProbes
			vpath.DataProbePending = !p.dataProbeSent.IsZero()
			for _, packet := range s.pending {
				if packet.primary == n && (packet.Kind == Data || packet.Kind == Datagram) {
					vpath.PendingBytes += uint64(len(packet.Payload))
				}
			}
			vpath.RTTMS = float64(p.rtt) / 1e6
			vpath.Ready = now.Sub(p.lastReply) < 2*time.Second
		}
		v.Paths = append(v.Paths, vpath)
	}

	for _, p := range s.pending {
		if a := now.Sub(p.created).Milliseconds(); a > v.OldestMS {
			v.OldestMS = a
		}
	}
	return v
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// ProbeData checks a whole working-size record without opening an exit flow.
// The caller owns probe cadence and network/budget permissions. One outstanding
// probe per generation is allowed; sending more probes cannot reset its age.
func (s *Session) ProbeData(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.paths[name]
	if p == nil {
		return net.ErrClosed
	}
	if !p.dataProbeSent.IsZero() {
		return nil
	}
	now := time.Now()
	stamp := now.UnixNano()
	if err := s.sendOn(name, p, frame(ping, 0, 0, 0, stamp, false, make([]byte, Chunk)), true); err != nil {
		return err
	}
	p.dataProbeStamp = stamp
	p.dataProbeSent = now
	return nil
}
