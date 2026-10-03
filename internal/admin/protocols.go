package admin

import (
	"context"
	"crypto/tls"
	"errors"
	"net/netip"
	"quiclab/internal/awgserver"
	"time"
)

func (u User) Allows(protocol string) bool { return awgserver.Enabled(u.Protocols, protocol) }
func (u User) QUICEnabled() bool           { return u.Allows("quic") }
func (u User) HTTPSEnabled() bool          { return u.Allows("https") }
func (u User) AWGEnabled() bool            { return u.Allows("awg") }
func (s *Store) ConfigureAWG(c *awgserver.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c == nil {
		return nil
	}
	if e := c.Validate(); e != nil {
		return e
	}
	s.awgConfig = c
	if s.state.AWG == nil {
		i, e := awgserver.NewIdentity()
		if e != nil {
			return e
		}
		s.state.AWG = i
		if e = s.save(); e != nil {
			if !statePublished(e) {
				s.state.AWG = nil
			}
			return e
		}
	}
	return nil
}
func (s *Store) provisionAWG(u *User) error {
	if u.Protocols != nil {
		if len(u.Protocols) == 0 || len(u.Protocols) > 4 {
			return errors.New("choose at least one protocol")
		}
		seen := map[string]bool{}
		for _, p := range u.Protocols {
			if seen[p] || (p != "quic" && p != "https" && p != "awg" && p != "vless") {
				return errors.New("invalid protocol selection")
			}
			seen[p] = true
		}
	}
	if !u.Allows("awg") {
		return nil
	}
	if s.awgConfig == nil || s.state.AWG == nil {
		return errors.New("AWG server is not configured")
	}
	if u.AWG != nil {
		return nil
	}
	network, _ := netip.ParsePrefix(s.awgConfig.Address)
	used := map[string]bool{network.Addr().String(): true}
	for _, v := range s.state.Devices {
		if v.AWG != nil {
			used[v.AWG.Address] = true
		}
	}
	for ip := network.Masked().Addr().Next(); network.Contains(ip.Next()); ip = ip.Next() {
		if !used[ip.String()] {
			p, e := awgserver.NewPeer(ip.String())
			if e != nil {
				return e
			}
			u.AWG = p
			return nil
		}
	}
	return errors.New("AWG address pool exhausted")
}
func (s *Store) SetProtocols(id string, protocols []string, disabled bool) error {
	return s.setUserAccess(id, protocols, &disabled, nil)
}

// setUserAccess persists the name and protocol selection together, then applies
// revocations. A worker failure can mean the durable edit was only partly applied.
func (s *Store) setUserAccess(id string, protocols []string, disabled *bool, name *string) error {
	s.mu.Lock()
	old, ok := s.state.Users[id]
	if !ok {
		s.mu.Unlock()
		return errors.New("unknown user")
	}
	u := old
	u.Protocols = append([]string{}, protocols...)
	if disabled != nil {
		u.Disabled = *disabled
	}
	if name != nil {
		if len(*name) == 0 || len(*name) > 100 {
			s.mu.Unlock()
			return errors.New("name must contain 1..100 bytes")
		}
		for otherID, other := range s.state.Users {
			if otherID != id && other.Name == *name {
				s.mu.Unlock()
				return errors.New("name already exists")
			}
		}
		u.Name = *name
	}
	previous := map[string]Device{}
	for deviceID, d := range s.state.Devices {
		if d.UserID == id {
			previous[deviceID] = d
		}
	}
	rollback := func() {
		s.state.Users[id] = old
		for deviceID, d := range previous {
			s.state.Devices[deviceID] = d
		}
	}
	needsAWG := false
	// Reserve each newly allocated address immediately, so siblings cannot share it.
	for deviceID, d := range previous {
		candidate := User{Protocols: u.Protocols, AWG: d.AWG}
		if e := s.provisionAWG(&candidate); e != nil {
			rollback()
			s.mu.Unlock()
			return e
		}
		d.AWG = candidate.AWG
		if e := s.provisionVLESS(&d, u); e != nil {
			rollback()
			s.mu.Unlock()
			return e
		}
		s.state.Devices[deviceID] = d
		needsAWG = needsAWG || d.AWG != nil
	}
	if legacy, ok := s.state.Devices[id]; ok {
		u.AWG = legacy.AWG
	}
	s.state.Users[id] = u
	saveErr := s.save()
	if e := saveErr; e != nil && !statePublished(e) {
		rollback()
		s.mu.Unlock()
		return e
	}
	closers := []func(){}
	for deviceID := range previous {
		for token, close := range s.active[deviceID] {
			if u.Disabled || !u.Allows(s.sessionProtocols[token]) {
				closers = append(closers, close)
				delete(s.active[deviceID], token)
				delete(s.sessionProtocols, token)
			}
		}
		if len(s.active[deviceID]) == 0 {
			delete(s.active, deviceID)
		}
	}
	reload := s.awgReload
	s.mu.Unlock()
	closeAll(closers)
	saveErr = errors.Join(saveErr, s.SyncVLESS(context.Background()))
	if needsAWG {
		return errors.Join(saveErr, reloadAWG(reload))
	}
	return saveErr
}
func (s *Store) AWGProfile(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, present := s.state.Devices[id]
	u, ok := s.state.Users[d.UserID]
	now := time.Now()
	if !present || !ok || d.Disabled || u.Disabled || !d.Expires.After(now) || !u.Expires.After(now) || !u.Allows("awg") || d.AWG == nil || s.awgConfig == nil || s.state.AWG == nil {
		return "", errors.New("AWG unavailable for this device")
	}
	return s.awgConfig.Client(*s.state.AWG, *d.AWG), nil
}
func (s *Store) RegisterProtocol(cs tls.ConnectionState, protocol string, close func()) (func(), error) {
	s.mu.Lock()
	d, e := s.deviceAllowed(cs)
	if e != nil {
		s.mu.Unlock()
		return nil, e
	}
	if !s.state.Users[d.UserID].Allows(protocol) {
		s.mu.Unlock()
		return nil, errors.New("protocol disabled for this user")
	}
	releaseAdmission, e := s.admission.Acquire(d.ID)
	if e != nil {
		s.mu.Unlock()
		return nil, e
	}
	id := d.ID
	token := randomID()
	if s.active[id] == nil {
		s.active[id] = map[string]func(){}
	}
	if s.sessionProtocols == nil {
		s.sessionProtocols = map[string]string{}
	}
	s.active[id][token] = func() { releaseAdmission(); close() }
	s.sessionProtocols[token] = protocol
	s.mu.Unlock()
	return func() {
		releaseAdmission()
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.active[id], token)
		delete(s.sessionProtocols, token)
		if len(s.active[id]) == 0 {
			delete(s.active, id)
		}
	}, nil
}
