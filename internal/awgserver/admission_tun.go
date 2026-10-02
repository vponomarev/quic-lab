package awgserver

import (
	"github.com/amnezia-vpn/amneziawg-go/tun"
	"net/netip"
	"sync"
	"time"
)

// AdmissionTUN gates both directions before plaintext reaches the kernel or
// encrypted outbound queues. The AWG device authenticates inbound packets and
// checks their source against the authenticated peer's allowed /32 before Write.
// Outbound traffic never establishes or refreshes authenticated activity.
type AdmissionTUN struct {
	tun.Device
	mu      sync.Mutex
	request func(string) (time.Duration, error)
	now     func() time.Time
	offline time.Duration
	ids     map[netip.Addr]string
	leases  map[string]awgLease
}
type awgLease struct{ expires, activity, retry time.Time }

func NewAdmissionTUN(inner tun.Device, request func(string) (time.Duration, error), offline time.Duration) *AdmissionTUN {
	return &AdmissionTUN{Device: inner, request: request, now: time.Now, offline: offline, ids: map[netip.Addr]string{}, leases: map[string]awgLease{}}
}
func (g *AdmissionTUN) Update(ids map[netip.Addr]string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ids = ids
	present := map[string]bool{}
	for _, id := range ids {
		present[id] = true
	}
	for id := range g.leases {
		if !present[id] {
			delete(g.leases, id)
		}
	}
}
func packetAddress(packet []byte, source bool) (netip.Addr, bool) {
	if len(packet) < 20 || packet[0]>>4 != 4 || int(packet[0]&15)*4 < 20 || int(packet[0]&15)*4 > len(packet) {
		return netip.Addr{}, false
	}
	offset := 16
	if source {
		offset = 12
	}
	return netip.AddrFrom4([4]byte(packet[offset : offset+4])), true
}
func (g *AdmissionTUN) renewLocked(id string, lease awgLease) awgLease {
	start := g.now()
	lease.retry = start.Add(time.Second)
	ttl, e := g.request(id)
	if e != nil || ttl <= 0 || ttl > 2*time.Second {
		lease.expires = time.Time{}
		return lease
	}
	// Start time is conservative even if IPC is delayed. Never grant time from
	// response receipt; broker reservations cannot expire before this deadline.
	lease.expires = start.Add(ttl)
	return lease
}
func (g *AdmissionTUN) Write(bufs [][]byte, offset int) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	accepted := make([][]byte, 0, len(bufs))
	for _, buf := range bufs {
		if offset < 0 || offset > len(buf) {
			continue
		}
		addr, ok := packetAddress(buf[offset:], true)
		if !ok {
			continue
		}
		id := g.ids[addr]
		if id == "" {
			continue
		}
		lease := g.leases[id]
		now := g.now()
		if !lease.expires.After(now) && !lease.retry.After(now) {
			lease = g.renewLocked(id, lease)
		}
		now = g.now()
		if lease.expires.After(now) {
			lease.activity = now
			accepted = append(accepted, buf)
		}
		g.leases[id] = lease
	}
	// Later admissions may delay the whole batch beyond an earlier deadline.
	live := accepted[:0]
	now := g.now()
	for _, buf := range accepted {
		addr, _ := packetAddress(buf[offset:], true)
		if g.leases[g.ids[addr]].expires.After(now) {
			live = append(live, buf)
		}
	}
	if len(live) == 0 {
		return len(bufs), nil
	}
	_, e := g.Device.Write(live, offset)
	return len(bufs), e
}
func (g *AdmissionTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	n, e := g.Device.Read(bufs, sizes, offset)
	g.mu.Lock()
	defer g.mu.Unlock()
	out := 0
	now := g.now()
	for i := 0; i < n; i++ {
		if offset < 0 || sizes[i] < 0 || offset+sizes[i] > len(bufs[i]) {
			continue
		}
		addr, ok := packetAddress(bufs[i][offset:offset+sizes[i]], false)
		if !ok {
			continue
		}
		id := g.ids[addr]
		lease := g.leases[id]
		if id == "" || !lease.expires.After(now) {
			continue
		}
		if out != i {
			copy(bufs[out][offset:], bufs[i][offset:offset+sizes[i]])
			sizes[out] = sizes[i]
		}
		out++
	}
	return out, e
}

// Tick renews only leases with recent authenticated inbound data. Expired broker
// reservations stop forwarding even if the last activity is within offline time.
func (g *AdmissionTUN) Tick() {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	for id, lease := range g.leases {
		if lease.activity.IsZero() || now.Sub(lease.activity) >= g.offline {
			delete(g.leases, id)
			continue
		}
		if !lease.retry.After(now) {
			g.leases[id] = g.renewLocked(id, lease)
		}
	}
}

func (g *AdmissionTUN) Allowed(id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.leases[id].expires.After(g.now())
}

// Native zero-length keepalives never cross the TUN. Advance confirmed activity
// only on a still-live admitted lease; registration, stale observations, and
// revoked/expired leases cannot create or resurrect a reservation here.
func (g *AdmissionTUN) ObserveActivity(peers []PeerStatus) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	for _, peer := range peers {
		lease, ok := g.leases[peer.ID]
		if !ok || !lease.expires.After(now) || !peer.Activity.After(lease.activity) || peer.Activity.After(now) {
			continue
		}
		lease.activity = peer.Activity
		g.leases[peer.ID] = lease
	}
}
