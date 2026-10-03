package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"quiclab/internal/vless"
	"quiclab/internal/vlessserver"
	"sort"
	"time"
)

var errVLESS = errors.New("VLESS configuration application not confirmed")

func (u User) VLESSEnabled() bool { return u.Allows("vless") }
func (s *Store) provisionVLESS(d *Device, u User) error {
	if !u.Allows("vless") || u.Disabled || d.Disabled {
		if d.VLESSUUID != "" {
			d.VLESSRevoked = true
		}
		return nil
	}
	if s.state.VLESS == nil {
		return errors.New("VLESS server is not configured")
	}
	if d.VLESSUUID == "" {
		count := 0
		for _, existing := range s.state.Devices {
			if existing.VLESSUUID != "" {
				count++
			}
		}
		if count >= vless.MaxServerClients {
			return errors.New("VLESS configured device limit reached")
		}
	}
	if d.VLESSUUID == "" || d.VLESSRevoked {
		key, e := newDeviceID()
		if e != nil {
			return errVLESS
		}
		d.VLESSUUID = key
		d.VLESSRevoked = false
	}
	return nil
}
func (s *Store) ConfigureVLESS(c *vlessserver.Config, client *vlessserver.ControlClient) error {
	if c == nil {
		return nil
	}
	if c.Validate() != nil || client == nil {
		return errVLESS
	}
	raw, _ := json.Marshal(c)
	var copyConfig vlessserver.Config
	json.Unmarshal(raw, &copyConfig)
	s.mu.Lock()
	s.vlessClient = client
	if s.state.VLESS == nil {
		s.state.VLESS = &copyConfig
		if e := s.save(); e != nil {
			if !statePublished(e) {
				s.state.VLESS = nil
			}
			s.mu.Unlock()
			return errVLESS
		}
	}
	s.mu.Unlock()
	return s.SyncVLESS(context.Background())
}
func (s *Store) snapshotVLESSLocked() vlessserver.Snapshot {
	snapshot := vlessserver.Snapshot{Config: *s.state.VLESS}
	for _, d := range s.state.Devices {
		if d.VLESSUUID == "" {
			continue
		}
		u, ok := s.state.Users[d.UserID]
		expires := d.Expires
		if ok && u.Expires.Before(expires) {
			expires = u.Expires
		}
		snapshot.Devices = append(snapshot.Devices, vlessserver.Device{ID: d.ID, UUID: d.VLESSUUID, Expires: expires, Disabled: !ok || d.Disabled || d.VLESSRevoked || u.Disabled || !u.Allows("vless")})
	}
	sort.Slice(snapshot.Devices, func(i, j int) bool { return snapshot.Devices[i].ID < snapshot.Devices[j].ID })
	return snapshot
}
func (s *Store) SyncVLESS(ctx context.Context) error {
	s.vlessSyncMu.Lock()
	defer s.vlessSyncMu.Unlock()
	s.mu.Lock()
	if s.state.VLESS == nil {
		s.mu.Unlock()
		return nil
	}
	client := s.vlessClient
	if client == nil {
		s.mu.Unlock()
		return errVLESS
	}
	snapshot := s.snapshotVLESSLocked()
	raw, e := json.Marshal(snapshot)
	if e != nil {
		s.mu.Unlock()
		return errVLESS
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	oldRevision, oldDigest := s.state.VLESSRevision, s.state.VLESSDigest
	if digest != oldDigest {
		s.state.VLESSRevision++
		s.state.VLESSDigest = digest
	}
	if s.state.VLESSRevision == 0 {
		s.state.VLESSRevision = 1
	}
	snapshot.Revision = s.state.VLESSRevision
	// Retry unconfirmed durability; unchanged successful reconciliations need no disk write.
	if digest != oldDigest || oldRevision == 0 || s.vlessWritePending {
		if e = s.save(); e != nil {
			if !statePublished(e) {
				s.state.VLESSRevision = oldRevision
				s.state.VLESSDigest = oldDigest
			}
			s.vlessWritePending = true
			s.vlessApplied = nil
			s.vlessError = true
			s.mu.Unlock()
			return errVLESS
		}
		s.vlessWritePending = false
	}
	s.mu.Unlock()
	e = client.Apply(ctx, snapshot)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vlessError = e != nil
	if e != nil {
		s.vlessApplied = nil
		return errVLESS
	}
	s.vlessApplied = &snapshot
	return nil
}
func (s *Store) WatchVLESS(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.SyncVLESS(ctx)
		}
	}
}
func (s *Store) VLESSProfile(id string) (string, error) {
	if e := s.SyncVLESS(context.Background()); e != nil {
		return "", e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.state.Devices[id]
	u, present := s.state.Users[d.UserID]
	now := time.Now()
	if !ok || !present || d.Disabled || d.VLESSRevoked || u.Disabled || !d.Expires.After(now) || !u.Expires.After(now) || !u.Allows("vless") || d.VLESSUUID == "" || s.vlessApplied == nil {
		return "", errVLESS
	}
	for _, applied := range s.vlessApplied.Devices {
		if applied.ID == id && applied.UUID == d.VLESSUUID && !applied.Disabled {
			return s.vlessApplied.Config.ClientURI(d.VLESSUUID, d.Name)
		}
	}
	return "", errVLESS
}
func (s *Store) StartVLESSAdmission(ctx context.Context) (func(), error) {
	stop, e := vlessserver.ServeAdmission(ctx, s.admissionDirectory(), func(ctx context.Context, id, uuid string) (time.Duration, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		d, ok := s.state.Devices[id]
		u, present := s.state.Users[d.UserID]
		now := time.Now()
		if ctx.Err() != nil || !ok || !present || d.Disabled || d.VLESSRevoked || u.Disabled || d.VLESSUUID != uuid || d.VLESSUUID == "" || !u.Allows("vless") || !d.Expires.After(now) || !u.Expires.After(now) {
			return 0, errVLESS
		}
		ttl := min(2*time.Second, time.Until(d.Expires), time.Until(u.Expires))
		if e := s.admission.Renew(id, ttl); e != nil {
			return 0, e
		}
		return ttl, nil
	})
	if e != nil {
		return nil, e
	}
	s.admission.Quarantine(3 * time.Second)
	return stop, nil
}

func (s *Store) UpdateVLESSConfig(c vlessserver.Config) error {
	if c.Validate() != nil {
		return errVLESS
	}
	raw, _ := json.Marshal(c)
	json.Unmarshal(raw, &c)
	s.mu.Lock()
	if s.vlessClient == nil {
		s.mu.Unlock()
		return errVLESS
	}
	old := s.state.VLESS
	s.state.VLESS = &c
	if e := s.save(); e != nil {
		if !statePublished(e) {
			s.state.VLESS = old
		}
		s.mu.Unlock()
		return errVLESS
	}
	s.mu.Unlock()
	return s.SyncVLESS(context.Background())
}
func (s *Store) VLESSStatus() (configured, applied bool, revision uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.VLESS != nil, s.vlessApplied != nil && !s.vlessError, s.state.VLESSRevision
}
func (s *Store) vlessConfig() vlessserver.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	var c vlessserver.Config
	if s.state.VLESS != nil {
		b, _ := json.Marshal(s.state.VLESS)
		json.Unmarshal(b, &c)
	}
	return c
}
