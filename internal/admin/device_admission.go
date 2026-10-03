package admin

import (
	"errors"
	"sync"
	"time"
)

const DefaultDeviceLimit = 15
const MaxDeviceLimit = 512

type Admission struct {
	mu      sync.Mutex
	limit   int
	devices map[string]int
	leases  map[string]time.Time
	now     func() time.Time
	ready   time.Time
}

func NewAdmission(limit int) *Admission {
	if limit <= 0 {
		limit = DefaultDeviceLimit
	}
	if limit > MaxDeviceLimit {
		limit = MaxDeviceLimit
	}
	return &Admission{limit: limit, devices: map[string]int{}, leases: map[string]time.Time{}, now: time.Now}
}
func (a *Admission) Quarantine(duration time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ready = a.now().Add(duration)
}
func (a *Admission) ReadyAt() time.Time { a.mu.Lock(); defer a.mu.Unlock(); return a.ready }
func (a *Admission) availableLocked(id string) error {
	now := a.now()
	if now.Before(a.ready) {
		return errors.New("admission recovering prior leases")
	}
	for id, expiry := range a.leases {
		if !expiry.After(now) {
			delete(a.leases, id)
		}
	}
	if id == "" {
		return errors.New("device identity required")
	}
	if a.devices[id] > 0 || a.leases[id].After(now) {
		return nil
	}
	distinct := len(a.devices)
	for id := range a.leases {
		if a.devices[id] == 0 {
			distinct++
		}
	}
	if distinct >= a.limit {
		return errors.New("active device limit reached")
	}
	return nil
}
func (a *Admission) Acquire(id string) (func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.availableLocked(id); e != nil {
		return nil, e
	}
	a.devices[id]++
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			a.devices[id]--
			if a.devices[id] == 0 {
				delete(a.devices, id)
			}
		})
	}, nil
}
func (a *Admission) Renew(id string, ttl time.Duration) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ttl <= 0 || ttl > 2*time.Second {
		return errors.New("invalid admission lease")
	}
	if e := a.availableLocked(id); e != nil {
		return e
	}
	a.leases[id] = a.now().Add(ttl)
	return nil
}
func (a *Admission) Count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	out := len(a.devices)
	for id, expiry := range a.leases {
		if expiry.After(now) && a.devices[id] == 0 {
			out++
		}
	}
	return out
}

// ConfigureDeviceLimit is called once before transports/admission listeners start.
func (s *Store) ConfigureDeviceLimit(limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.admission = NewAdmission(limit)
}
