package admin

import (
	"context"
	"encoding/json"
	"errors"
	"quiclab/internal/vlessserver"
	"time"
)

type VLESSRouteController interface {
	Validate(vlessserver.Config) error
	Apply(context.Context, vlessserver.Config, uint64) error
}
type vlessPendingConfig struct {
	Previous vlessserver.Config `json:"previous"`
}

func (s *Store) SetVLESSRouteController(c VLESSRouteController) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vlessRoutes = c
}
func (s *Store) VLESSConfig() vlessserver.Config { return s.vlessConfig() }
func cloneVLESSConfig(c vlessserver.Config) vlessserver.Config {
	raw, _ := json.Marshal(c)
	var out vlessserver.Config
	json.Unmarshal(raw, &out)
	return out
}
func (s *Store) ApplyVLESSConfig(ctx context.Context, c vlessserver.Config, expectedRevision uint64) error {
	if c.Validate() != nil {
		return errVLESS
	}
	s.vlessApplyMu.Lock()
	defer s.vlessApplyMu.Unlock()
	s.mu.Lock()
	if s.state.VLESS == nil || s.vlessClient == nil || s.state.VLESSPending != nil {
		s.mu.Unlock()
		return errVLESS
	}
	if expectedRevision != 0 && expectedRevision != s.state.VLESSRevision {
		s.mu.Unlock()
		return errors.New("VLESS settings changed; reload before applying")
	}
	routes := s.vlessRoutes
	client := s.vlessClient
	s.mu.Unlock()
	if err := client.Preflight(ctx, c); err != nil {
		return err
	}
	if routes != nil {
		if err := routes.Validate(c); err != nil {
			return err
		}
	}
	s.mu.Lock()
	// Revisions may advance while preflight runs, for example on revocation.
	if expectedRevision != 0 && expectedRevision != s.state.VLESSRevision {
		s.mu.Unlock()
		return errors.New("VLESS settings changed; reload before applying")
	}
	previous := cloneVLESSConfig(*s.state.VLESS)
	candidate := cloneVLESSConfig(c)
	s.state.VLESSPending = &vlessPendingConfig{Previous: previous}
	s.state.VLESS = &candidate
	s.vlessApplied = nil
	err := s.save()
	if err != nil && !statePublished(err) {
		s.state.VLESS = &previous
		s.state.VLESSPending = nil
		s.vlessError = true
		s.mu.Unlock()
		return errVLESS
	}
	s.mu.Unlock()
	if err == nil {
		err = s.SyncVLESS(ctx)
	}
	if err == nil {
		s.mu.Lock()
		pending := s.state.VLESSPending
		s.state.VLESSPending = nil
		err = s.save()
		if err != nil {
			s.state.VLESSPending = pending
			s.vlessError = true
		}
		s.mu.Unlock()
		if err == nil {
			return nil
		}
	}
	// Roll back only configuration; snapshots always retain the latest identities.
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if rollbackErr := s.recoverVLESSConfig(rollbackCtx); rollbackErr != nil {
		return errors.New("VLESS apply failed; recovery required")
	}
	return errors.New("VLESS apply failed; previous settings restored")
}
func (s *Store) recoverVLESSConfig(ctx context.Context) error {
	s.mu.Lock()
	if s.state.VLESSPending == nil {
		s.mu.Unlock()
		return nil
	}
	previous := cloneVLESSConfig(s.state.VLESSPending.Previous)
	s.state.VLESS = &previous
	s.vlessApplied = nil
	// Force a newer revision even if the candidate never reached the worker.
	s.state.VLESSDigest = ""
	if err := s.save(); err != nil {
		s.vlessError = true
		s.mu.Unlock()
		return errVLESS
	}
	s.mu.Unlock()
	if err := s.SyncVLESS(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.state.VLESSPending
	s.state.VLESSPending = nil
	if err := s.save(); err != nil {
		s.state.VLESSPending = pending
		s.vlessError = true
		return errVLESS
	}
	return nil
}
