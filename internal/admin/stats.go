package admin

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"sync"
	"time"
)

type trafficBucket struct {
	Second int64
	TX, RX uint64
}
type liveConnection struct {
	Transport string
	Connected time.Time
	peer      func() string
}
type userTraffic struct {
	buckets [600]trafficBucket
	live    map[string]liveConnection
}
type ConnectionStats struct {
	Transport, Source string
	Connected         time.Time
}
type UserStats struct {
	RateTX, RateRX uint64
	TX, RX         uint64
	Connections    []ConnectionStats
}

func (s UserStats) RateText() string {
	return "↑ " + byteText(s.RateTX) + "/с · ↓ " + byteText(s.RateRX) + "/с"
}
func (s UserStats) TXText() string { return byteText(s.TX) }
func (s UserStats) RXText() string { return byteText(s.RX) }
func byteText(n uint64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	for _, unit := range units {
		v /= 1024
		if v < 1024 || unit == "TiB" {
			return fmt.Sprintf("%.1f %s", v, unit)
		}
	}
	return ""
}
func sourceIP(peer string) string {
	h, _, e := net.SplitHostPort(peer)
	if e == nil {
		return h
	}
	return peer
}
func (u *userTraffic) add(now time.Time, tx, rx int) {
	second := now.Unix()
	b := &u.buckets[second%600]
	if b.Second != second {
		*b = trafficBucket{Second: second}
	}
	if tx > 0 {
		b.TX += uint64(tx)
	}
	if rx > 0 {
		b.RX += uint64(rx)
	}
}

// snapshot is called with Store.mu held. Peers report the transport's current path.
func (s *Store) snapshot(id string, now time.Time) UserStats {
	out := UserStats{}
	u := s.stats[id]
	if u == nil {
		return out
	}
	for _, b := range u.buckets {
		if b.Second > now.Unix()-600 && b.Second <= now.Unix() {
			out.TX += b.TX
			out.RX += b.RX
			if b.Second > now.Unix()-5 {
				out.RateTX += b.TX
				out.RateRX += b.RX
			}
		}
	}
	out.RateTX /= 5
	out.RateRX /= 5
	for _, c := range u.live {
		out.Connections = append(out.Connections, ConnectionStats{c.Transport, sourceIP(c.peer()), c.Connected})
	}
	sort.Slice(out.Connections, func(i, j int) bool { return out.Connections[i].Connected.Before(out.Connections[j].Connected) })
	return out
}

// Track records successful authenticated tunnels, not TLS handshakes or echo clients.
// The byte window stays in memory; last connection metadata survives server restarts.
func (s *Store) Track(cs tls.ConnectionState, transport string, peer func() string) (func(int, int), func()) {
	s.mu.Lock()
	id, e := s.allowed(cs)
	if e != nil {
		s.mu.Unlock()
		return nil, func() {}
	}
	if s.stats == nil {
		s.stats = make(map[string]*userTraffic)
	}
	if s.stats[id] == nil {
		s.stats[id] = &userTraffic{live: make(map[string]liveConnection)}
	}
	now := time.Now().UTC()
	token := randomID()
	traffic := s.stats[id]
	traffic.live[token] = liveConnection{transport, now, peer}
	user := s.state.Users[id]
	user.LastConnected = now
	user.LastTransport = transport
	user.LastSource = sourceIP(peer())
	s.state.Users[id] = user
	if e := s.save(); e != nil {
		slog.Error("save_connection_metadata", "error", e)
	}
	s.mu.Unlock()
	var once sync.Once
	return func(tx, rx int) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.stats[id] == traffic {
				if _, ok := traffic.live[token]; ok {
					traffic.add(time.Now(), tx, rx)
				}
			}
		}, func() {
			once.Do(func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				delete(traffic.live, token)
				if user, ok := s.state.Users[id]; ok && user.LastConnected.Equal(now) {
					user.LastSource = sourceIP(peer())
					s.state.Users[id] = user
					if e := s.save(); e != nil {
						slog.Error("save_connection_metadata", "error", e)
					}
				}
			})
		}
}
