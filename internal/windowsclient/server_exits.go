package windowsclient

import (
	"context"
	"errors"
	"quiclab/internal/exitcontrol"
	"quiclab/internal/vpnclient"
)

type ServerExitRequest struct {
	ExpectedRevision uint64              `json:"expected_revision"`
	Change           *exitcontrol.Change `json:"change,omitempty"`
}

// Serialize against profile activation. Control HTTPS requires a stopped VPN so
// an exit change cannot tear down the connection carrying its own acknowledgement.
func (m *SettingsManager) ServerExits(ctx context.Context, q ServerExitRequest) (exitcontrol.View, error) {
	if err := m.lock(ctx); err != nil {
		return exitcontrol.View{}, err
	}
	defer func() { <-m.gate }()
	run, done, err := m.begin(ctx)
	if err != nil {
		return exitcontrol.View{}, err
	}
	defer done()
	m.mu.RLock()
	revision, cfg := m.doc.Revision, m.doc.Settings.ServerControl.Clone()
	m.mu.RUnlock()
	if q.ExpectedRevision != revision {
		return exitcontrol.View{}, errors.New("revision_conflict")
	}
	if m.Snapshot().State != vpnclient.Stopped {
		return exitcontrol.View{}, errors.New("stop_before_apply")
	}
	if cfg == nil {
		return exitcontrol.View{}, exitcontrol.ErrUnsupported
	}
	if q.Change != nil {
		return m.exitClient.Select(run, *cfg, *q.Change)
	}
	return m.exitClient.Fetch(run, *cfg)
}
