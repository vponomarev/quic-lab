package admin

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"quiclab/internal/awgserver"
	"time"
)

func (s *Store) ObserveAWG(status awgserver.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if s.stats == nil {
		s.stats = map[string]*userTraffic{}
	}
	for _, traffic := range s.stats {
		delete(traffic.live, "awg")
	}
	if status.Updated.IsZero() || now.Sub(status.Updated) > 5*time.Second {
		return
	}
	s.awgUpdated = status.Updated
	if s.awgPrevious == nil || !s.awgStarted.Equal(status.Started) {
		s.awgPrevious = map[string]awgserver.PeerStatus{}
		s.awgStarted = status.Started
	}
	changed := false
	for _, p := range status.Peers {
		u, ok := s.state.Users[p.ID]
		if !ok || u.Disabled || !u.Allows("awg") {
			continue
		}
		if s.stats[p.ID] == nil {
			s.stats[p.ID] = &userTraffic{live: map[string]liveConnection{}}
		}
		traffic := s.stats[p.ID]
		previous, known := s.awgPrevious[p.ID]
		if known {
			var tx, rx uint64
			if p.TX >= previous.TX {
				tx = p.TX - previous.TX
			}
			if p.RX >= previous.RX {
				rx = p.RX - previous.RX
			}
			traffic.add(now, int(tx), int(rx))
		}
		s.awgPrevious[p.ID] = p
		if !p.Activity.IsZero() && now.Sub(p.Activity) < 3*time.Minute {
			source := p.Source
			traffic.live["awg"] = liveConnection{"AmneziaWG · недавняя активность", p.Handshake, func() string { return source }}
		}
		if p.Handshake.After(u.LastConnected) {
			u.LastConnected = p.Handshake
			u.LastTransport = "AmneziaWG"
			u.LastSource = sourceIP(p.Source)
			s.state.Users[p.ID] = u
			changed = true
		} else if u.LastTransport == "AmneziaWG" && u.LastSource != sourceIP(p.Source) && !p.Handshake.IsZero() {
			u.LastSource = sourceIP(p.Source)
			s.state.Users[p.ID] = u
			changed = true
		}
	}
	if changed {
		if e := s.save(); e != nil {
			slog.Error("persist AWG statistics", "error", e)
		}
	}
}
func (s *Store) WatchAWG(ctx context.Context, dir string) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		var status awgserver.Status
		b, e := os.ReadFile(filepath.Join(dir, "awg-status.json"))
		if e == nil {
			json.Unmarshal(b, &status)
		}
		s.ObserveAWG(status)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
