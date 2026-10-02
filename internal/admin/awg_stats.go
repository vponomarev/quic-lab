package admin

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"quiclab/internal/awgserver"
	"strings"
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
		for token := range traffic.live {
			if strings.HasPrefix(token, "awg:") || token == "awg" {
				delete(traffic.live, token)
			}
		}
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
		d, present := s.state.Devices[p.ID]
		u, ok := s.state.Users[d.UserID]
		if !present || !ok || d.Disabled || u.Disabled || !d.Expires.After(now) || !u.Expires.After(now) || !u.Allows("awg") {
			continue
		}
		if s.stats[d.UserID] == nil {
			s.stats[d.UserID] = &userTraffic{live: map[string]liveConnection{}}
		}
		traffic := s.stats[d.UserID]
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
		if (!status.AdmissionEnforced || p.Admitted) && s.awgConfig != nil && !p.Activity.IsZero() && now.Sub(p.Activity) < s.awgConfig.OfflineAfter() {
			source := p.Source
			traffic.live["awg:"+d.ID] = liveConnection{"AmneziaWG · канал подтверждён", p.Handshake, func() string { return source }}
		}
		if p.Handshake.After(u.LastConnected) {
			u.LastConnected = p.Handshake
			u.LastTransport = "AmneziaWG"
			u.LastSource = sourceIP(p.Source)
			s.state.Users[d.UserID] = u
			changed = true
		} else if u.LastTransport == "AmneziaWG" && u.LastSource != sourceIP(p.Source) && !p.Handshake.IsZero() {
			u.LastSource = sourceIP(p.Source)
			s.state.Users[d.UserID] = u
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
