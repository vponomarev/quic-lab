package trafficbudget

import (
	"errors"
	"sync"
)

var ErrBlocked = errors.New("shared LTE budget exhausted")

// Meter owns every cellular socket in a run, including draining sessions.
type Meter struct {
	Ledger  *Ledger
	mu      sync.Mutex
	stopped bool
	next    uint64
	closers map[uint64]func()
}

func NewMeter(l *Ledger) *Meter { return &Meter{Ledger: l, closers: make(map[uint64]func())} }
func (m *Meter) Allowed() bool {
	m.mu.Lock()
	stopped := m.stopped
	m.mu.Unlock()
	return !stopped && !m.Ledger.Snapshot().Blocked
}
func (m *Meter) Register(close func()) func() {
	m.mu.Lock()
	if m.stopped || m.Ledger.Snapshot().Blocked {
		m.mu.Unlock()
		close()
		return func() {}
	}
	m.next++
	id := m.next
	m.closers[id] = close
	m.mu.Unlock()
	return func() { m.mu.Lock(); delete(m.closers, id); m.mu.Unlock() }
}
func (m *Meter) stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	closers := m.closers
	m.closers = make(map[uint64]func())
	m.mu.Unlock()
	for _, close := range closers {
		close()
	}
}
func (m *Meter) Check() {
	if m.Ledger.Snapshot().Blocked {
		m.stop()
	}
}

// A record that cannot fit stops LTE without charging bytes that were not sent.
// Pending reservations alone must not permanently exhaust the budget.
func (m *Meter) Reserve(n uint64, class Class) (uint64, bool) {
	if !m.Allowed() {
		return 0, false
	}
	id, ok := m.Ledger.Reserve(n, class)
	if !ok && n > 0 && valid(class) {
		s := m.Ledger.Snapshot()
		if s.Limit > 0 && s.Reserved == 0 && (s.Used >= s.Limit || n > s.Limit-s.Used) {
			m.stop()
		}
	}
	return id, ok
}
func (m *Meter) Commit(ticket, n uint64)       { m.Ledger.Commit(ticket, n); m.Check() }
func (m *Meter) Receive(n uint64, class Class) { m.Ledger.ObserveReceived(n, class); m.Check() }
