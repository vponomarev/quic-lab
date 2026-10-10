package mdm

import (
	"encoding/json"
	"errors"
	"quiclab/internal/backup"
	"time"
)

// BackupState operates on detached, authenticated snapshot bytes, never live state.
func BackupState(raw []byte, kind backup.Kind, restoring bool) ([]byte, error) {
	if len(raw) > 64<<20 {
		return nil, errors.New("MDM backup state too large")
	}
	var state snapshot
	if json.Unmarshal(raw, &state) != nil || state.Version != 1 || state.Bindings == nil || state.Invitations == nil {
		return nil, errors.New("invalid MDM backup state")
	}
	for id, b := range state.Bindings {
		if b.Binding.ID != id || b.Binding.Epoch < 0 || !validSecret(b.SecretHash) || b.Seen == nil {
			return nil, errors.New("invalid MDM backup binding")
		}
		if kind == backup.Config {
			b.Report = nil
			b.ReportHash = ""
			b.ReceivedAt = time.Time{}
			b.Audit = nil
			b.TelemetryDropped = 0
		}
		if restoring {
			b.Commands = nil
		}
		state.Bindings[id] = b
	}
	return json.Marshal(state)
}
