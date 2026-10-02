package admin

import (
	"crypto/tls"
	"errors"
	"quiclab/internal/debugcapture"
	"sync"
)

var errMultiplexedCapture = errors.New("capture unavailable: active multiplexed bond; stop the bond before starting capture")

type captureState struct {
	mu      sync.Mutex
	manager *debugcapture.Manager
	// Logical sessions, keyed by device; values retain the owning user because
	// the existing inner capture API records all devices of a user.
	active map[string]map[string]string
}

func (s *Store) captureState() *captureState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.capture == nil {
		s.capture = &captureState{active: make(map[string]map[string]string)}
	}
	return s.capture
}

func (s *Store) ConfigureCapture(m *debugcapture.Manager) {
	state := s.captureState()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.manager = m
	if m != nil {
		for _, sessions := range state.active {
			for _, user := range sessions {
				m.StopMultiplexedScope(user)
			}
		}
	}
}

func (s *Store) captureTarget(target string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if device, ok := s.state.Devices[target]; ok {
		return device.UserID
	}
	return target
}

func (state *captureState) eligible(user string) error {
	for _, sessions := range state.active {
		for _, owner := range sessions {
			if user == "" || user == owner {
				return errMultiplexedCapture
			}
		}
	}
	return nil
}

// CaptureEligible checks logical session state, never a protocol display name.
// An empty target checks the shared VPN listener; a device resolves to its user
// because existing captures include every device belonging to that user.
func (s *Store) CaptureEligible(target string) error {
	user := s.captureTarget(target)
	state := s.captureState()
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.eligible(user)
}

// WithCaptureEligible serializes capture creation against bond entry. Store.mu
// is not held across the callback, which may authenticate capture listeners.
func (s *Store) WithCaptureEligible(target string, start func() error) error {
	user := s.captureTarget(target)
	state := s.captureState()
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := state.eligible(user); err != nil {
		return err
	}
	return start()
}

// TrackMultiplexed is called once for an authenticated logical bond, not once
// per path. Capture shutdown cannot close or reject an otherwise valid VPN.
func (s *Store) TrackMultiplexed(cs tls.ConnectionState) (func(), error) {
	device, err := s.DeviceForTLS(cs)
	if err != nil {
		return nil, err
	}
	state := s.captureState()
	state.mu.Lock()
	token := randomID()
	if state.active[device.ID] == nil {
		state.active[device.ID] = make(map[string]string)
	}
	state.active[device.ID][token] = device.UserID
	if state.manager != nil {
		state.manager.StopMultiplexedScope(device.UserID)
	}
	state.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			state.mu.Lock()
			defer state.mu.Unlock()
			delete(state.active[device.ID], token)
			if len(state.active[device.ID]) == 0 {
				delete(state.active, device.ID)
			}
		})
	}, nil
}
