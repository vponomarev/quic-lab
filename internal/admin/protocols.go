package admin

import (
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
			s.state.AWG = nil
			return e
		}
	}
	return nil
}
func (s *Store) provisionAWG(u *User) error {
	if u.Protocols != nil {
		if len(u.Protocols) == 0 || len(u.Protocols) > 3 {
			return errors.New("choose at least one protocol")
		}
		seen := map[string]bool{}
		for _, p := range u.Protocols {
			if seen[p] || (p != "quic" && p != "https" && p != "awg") {
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
	for _, v := range s.state.Users {
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
	s.mu.Lock()
	old, ok := s.state.Users[id]
	if !ok {
		s.mu.Unlock()
		return errors.New("unknown user")
	}
	u := old
	u.Protocols = append([]string{}, protocols...)
	u.Disabled = disabled
	if e := s.provisionAWG(&u); e != nil {
		s.mu.Unlock()
		return e
	}
	s.state.Users[id] = u
	if e := s.save(); e != nil {
		s.state.Users[id] = old
		s.mu.Unlock()
		return e
	}
	var closeSessions []func()
	for token, close := range s.active[id] {
		p := s.sessionProtocols[token]
		if disabled || !u.Allows(p) {
			closeSessions = append(closeSessions, close)
		}
	}
	s.mu.Unlock()
	for _, close := range closeSessions {
		close()
	}
	return nil
}
func (s *Store) AWGProfile(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.state.Users[id]
	if !ok || u.Disabled || !u.Expires.After(time.Now()) || !u.Allows("awg") || u.AWG == nil || s.awgConfig == nil || s.state.AWG == nil {
		return "", errors.New("AWG unavailable for this user")
	}
	return s.awgConfig.Client(*s.state.AWG, *u.AWG), nil
}
func (s *Store) RegisterProtocol(cs tls.ConnectionState, protocol string, close func()) (func(), error) {
	s.mu.Lock()
	id, e := s.allowed(cs)
	if e != nil {
		s.mu.Unlock()
		return nil, e
	}
	if !s.state.Users[id].Allows(protocol) {
		s.mu.Unlock()
		return nil, errors.New("protocol disabled for this user")
	}
	token := randomID()
	if s.active[id] == nil {
		s.active[id] = map[string]func(){}
	}
	if s.sessionProtocols == nil {
		s.sessionProtocols = map[string]string{}
	}
	s.active[id][token] = close
	s.sessionProtocols[token] = protocol
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.active[id], token)
		delete(s.sessionProtocols, token)
		if len(s.active[id]) == 0 {
			delete(s.active, id)
		}
	}, nil
}
