// Package bond joins authenticated packet paths into a bounded, resumable session.
// Paths must encrypt/authenticate packets and enforce congestion control themselves.
package bond

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
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
	*traffic
	p                                                 Path
	rtt, variance                                     time.Duration
	lastReply, lastSend, healthySince, penalizedUntil time.Time
	failures                                          int
}
type PathStats struct {
	RXCopies  uint64  `json:"rx_copies"`
	RXControl uint64  `json:"rx_control"`
	Name      string  `json:"name"`
	RTTMS     float64 `json:"rtt_ms"`
	Ready     bool    `json:"ready"`
	Sent      uint64  `json:"sent"`
	Received  uint64  `json:"received"`
	Copies    uint64  `json:"copies"`
	Probes    uint64  `json:"probes"`
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
	cellBlocked   bool
	totals        map[string]*traffic
	recordTimeout time.Duration
	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	paths         map[string]*pathState
	pending       map[uint64]*packet
	perFlow       map[uint32]int
	next          uint64
	changed       chan struct{}
	deliver       func(Record) bool
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
	c, cancel := context.WithCancel(ctx)
	s := &Session{totals: map[string]*traffic{}, recordTimeout: 30 * time.Second, ctx: c, cancel: cancel, paths: map[string]*pathState{}, pending: map[uint64]*packet{}, perFlow: map[uint32]int{}, changed: make(chan struct{}), deliver: deliver, emptySince: time.Now()}
	go s.run()
	return s
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
func (s *Session) BlockCell()     { s.mu.Lock(); s.cellBlocked = true; s.mu.Unlock() }
func (s *Session) noteDuplicate() { s.mu.Lock(); s.duplicate++; s.mu.Unlock() }
func (s *Session) sendOn(name string, p *pathState, b []byte, control bool) error {
	if name == "cell" && (s.cellBlocked || s.cellLimit > 0 && s.cellSent+uint64(len(b)) > s.cellLimit) {
		s.cellBlocked = true
		return errors.New("LTE session budget exhausted")
	}
	if e := p.p.SendDatagram(b); e != nil {
		return e
	}
	p.sent += uint64(len(b))
	p.lastSend = time.Now()
	if name == "cell" {
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
	if name != "wifi" && name != "cell" {
		return errors.New("invalid path")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return net.ErrClosed
	}
	if name == "cell" && (s.cellBlocked || s.cellLimit > 0 && s.cellSent >= s.cellLimit) {
		return errors.New("LTE session budget exhausted")
	}
	if old := s.paths[name]; old != nil {
		old.p.Close()
	}
	now := time.Now()
	healthy := now
	// A first Wi-Fi path may be used immediately; a recovered path must prove stability.
	if s.next == 0 && len(s.paths) == 0 {
		healthy = now.Add(-10 * time.Second)
	}
	if s.totals[name] == nil {
		s.totals[name] = &traffic{}
	}
	ps := &pathState{traffic: s.totals[name], p: p, rtt: 100 * time.Millisecond, variance: 25 * time.Millisecond, lastReply: now, healthySince: healthy}
	s.paths[name] = ps
	s.emptySince = time.Time{}
	s.signal()
	go s.receive(name, ps)
	return nil
}
func (s *Session) RemovePath(name string) {
	s.mu.Lock()
	if p := s.paths[name]; p != nil {
		delete(s.paths, name)
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
		if len(s.pending) < maxPending && s.perFlow[r.Flow] < 64 {
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
	w, c := s.paths["wifi"], s.paths["cell"]
	if s.cellBlocked || s.cellLimit > 0 && s.cellSent >= s.cellLimit {
		c = nil
	}
	if w != nil {
		if c == nil {
			return "wifi"
		}
		if now.Sub(c.lastReply) > 2*time.Second && now.Sub(w.lastReply) < 2*time.Second {
			return "wifi"
		}
		healthy := now.Sub(w.healthySince)
		if now.After(w.penalizedUntil) && now.Sub(w.lastReply) < 2*time.Second && healthy > 5*time.Second && w.rtt <= max(150*time.Millisecond, c.rtt*3/2) {
			if healthy > 8*time.Second {
				return "wifi"
			}
			if now.Sub(s.lastTrial) > 100*time.Millisecond {
				s.lastTrial = now
				return "wifi"
			}
		}
	}
	if c != nil {
		return "cell"
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
				if now.Sub(s.emptySince) > 30*time.Second {
					s.cancel()
				}
			} else {
				s.emptySince = time.Time{}
			}
			for name, p := range s.paths {
				if name == "cell" && s.cellBlocked {
					delete(s.paths, name)
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
				if !p.copied && now.Sub(p.last) > threshold {
					other := "wifi"
					if p.primary == "wifi" {
						other = "cell"
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
		ps.received += uint64(len(b))
		if kind >= ack {
			ps.rxControl += uint64(len(b))
		} else if b[1] == 1 {
			ps.rxCopies += uint64(len(b))
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
			if p := s.pending[id]; kind == ack && p != nil {
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
			reply := frame(pong, 0, 0, 0, stamp, false, nil)
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
	for _, n := range []string{"wifi", "cell"} {
		total := s.totals[n]
		if total == nil {
			continue
		}
		vpath := PathStats{Name: n, Sent: total.sent, Received: total.received, Copies: total.copies, Probes: total.probes, RXCopies: total.rxCopies, RXControl: total.rxControl}
		if p := s.paths[n]; p != nil {
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
