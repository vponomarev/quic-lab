package admin

import "time"

type vlessPeerKey struct{ user, device, source string }
type vlessPeer struct{ connected, expires time.Time }

// Called only after authenticated device admission, with Store.mu held.
// A closed transport disappears after its admission lease (at most two seconds).
// The bounded diagnostic cache never changes admission or traffic policy.
func (s *Store) observeVLESSPeerLocked(user, device, source string, now time.Time, ttl time.Duration) {
	if source == "" {
		return
	}
	if s.vlessPeers == nil {
		s.vlessPeers = make(map[vlessPeerKey]vlessPeer)
	}
	key := vlessPeerKey{user, device, source}
	p, exists := s.vlessPeers[key]
	if exists && !p.expires.After(now) {
		delete(s.vlessPeers, key)
		exists = false
	}
	if !exists {
		for k, v := range s.vlessPeers {
			if !v.expires.After(now) {
				delete(s.vlessPeers, k)
			}
		}
		if len(s.vlessPeers) >= 4096 {
			return
		}
		p.connected = now
	}
	p.expires = now.Add(ttl)
	s.vlessPeers[key] = p
}
func (s *Store) vlessPeerStatsLocked(user string, now time.Time) []ConnectionStats {
	var peers []ConnectionStats
	for key, p := range s.vlessPeers {
		if !p.expires.After(now) {
			delete(s.vlessPeers, key)
			continue
		}
		if key.user == user {
			peers = append(peers, ConnectionStats{Transport: "vless", Source: key.source, Connected: p.connected})
		}
	}
	return peers
}
