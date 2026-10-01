// Package trafficbudget accounts one VPN run across all exits and transports.
package trafficbudget

import (
	"math"
	"sync"
)

type Class string

const (
	User    Class = "user"
	Control Class = "control"
	Copy    Class = "copy"
	Config  Class = "config"
	APK     Class = "apk"
)

func valid(c Class) bool {
	switch c {
	case User, Control, Copy, Config, APK:
		return true
	}
	return false
}

type Snapshot struct {
	Epoch    string           `json:"epoch"`
	Limit    uint64           `json:"limit"`
	Used     uint64           `json:"used"`
	Reserved uint64           `json:"reserved"`
	Blocked  bool             `json:"blocked"`
	ByClass  map[Class]uint64 `json:"by_class"`
}
type reservation struct {
	bytes uint64
	class Class
}
type Ledger struct {
	mu                          sync.Mutex
	epoch                       string
	limit, used, reserved, next uint64
	pending                     map[uint64]reservation
	classes                     map[Class]uint64
}

func New(epoch string, limit uint64) *Ledger {
	return &Ledger{epoch: epoch, limit: limit, pending: make(map[uint64]reservation), classes: make(map[Class]uint64)}
}
func add(a, b uint64) uint64 {
	if b > math.MaxUint64-a {
		return math.MaxUint64
	}
	return a + b
}

// Reserve is atomic across transports. Zero-sized/invalid requests are rejected.
// Limit zero means unlimited traffic, not unlimited counter arithmetic.
func (l *Ledger) Reserve(n uint64, class Class) (uint64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n == 0 || !valid(class) || l.next == math.MaxUint64 || n > math.MaxUint64-l.reserved {
		return 0, false
	}
	if l.limit != 0 {
		if l.used >= l.limit || l.reserved > l.limit-l.used || n > l.limit-l.used-l.reserved {
			return 0, false
		}
	}
	l.next++
	l.pending[l.next] = reservation{n, class}
	l.reserved += n
	return l.next, true
}

// Commit consumes a ticket once. A partial/failed write releases its unused
// reservation. Actual overruns are accounted rather than hidden or wrapped.
func (l *Ledger) Commit(ticket, actual uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.pending[ticket]
	if !ok {
		return
	}
	delete(l.pending, ticket)
	l.reserved -= r.bytes
	l.observe(actual, r.class)
}
func (l *Ledger) observe(n uint64, class Class) {
	if !valid(class) {
		class = User
	} // Never lose accounting for received bytes.
	l.used = add(l.used, n)
	l.classes[class] = add(l.classes[class], n)
}

// Incoming bytes can exceed a finite limit because packets may be in flight.
// Transport owners (C2) must stop cellular use when Snapshot reports Blocked.
func (l *Ledger) ObserveReceived(n uint64, class Class) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.observe(n, class)
}
func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	classes := make(map[Class]uint64, len(l.classes))
	for k, v := range l.classes {
		classes[k] = v
	}
	return Snapshot{Epoch: l.epoch, Limit: l.limit, Used: l.used, Reserved: l.reserved, Blocked: l.limit != 0 && l.used >= l.limit, ByClass: classes}
}
