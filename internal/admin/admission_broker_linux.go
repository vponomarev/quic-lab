//go:build linux

package admin

import (
	"context"
	"errors"
	"path/filepath"
	"quiclab/internal/awgserver"
	"time"
)

func (s *Store) admissionDirectory() string  { return filepath.Dir(s.path) }
func (s *Store) AdmissionReadyAt() time.Time { return s.admission.ReadyAt() }
func (s *Store) StartAWGAdmission(ctx context.Context) (func(), error) {
	ready := make(chan struct{})
	stop, e := awgserver.ServeAdmission(ctx, s.admissionDirectory(), func(id string) (time.Duration, error) {
		select {
		case <-ready:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		d, present := s.state.Devices[id]
		u, ok := s.state.Users[d.UserID]
		now := time.Now()
		if !present || !ok || d.Disabled || u.Disabled || !d.Expires.After(now) || !u.Expires.After(now) || !u.Allows("awg") {
			return 0, errors.New("device unavailable")
		}
		if e := s.admission.Renew(id, awgserver.AdmissionLeaseTTL); e != nil {
			return 0, e
		}
		return awgserver.AdmissionLeaseTTL, nil
	})
	if e != nil {
		return nil, e
	}
	// Wait longer than every prior worker's conservative cached deadline before
	// any transport opens a new slot. Binding failure leaves live admission alone.
	s.admission.Quarantine(3 * time.Second)
	close(ready)
	return stop, nil
}
