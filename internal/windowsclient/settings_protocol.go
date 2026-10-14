package windowsclient

import (
	"context"
	"errors"
	"quiclab/internal/windowsengine"
)

func NewSettingsHandler(m *SettingsManager) *Handler { return &Handler{settings: m} }
func (h *Handler) handleSettings(ctx context.Context, q Request) Response {
	r := Response{Version: 1, ID: q.ID, Capabilities: []string{"settings-v1"}}
	var err error
	switch q.Command {
	case "status":
	case "read_settings":
		v := h.settings.Read()
		r.Settings = &v
	case "start":
		err = h.settings.Start(ctx)
	case "stop":
		err = h.settings.Stop(ctx)
	case "validate_settings":
		err = h.settings.Validate(*q.Settings)
	case "apply_settings":
		err = h.settings.Apply(ctx, *q.Settings)
	default:
		err = errors.New("unsupported_command")
	}
	r.Status = h.settings.Snapshot()
	r.SavedRevision = h.settings.Read().Revision
	r.OK = err == nil
	if err != nil {
		var field *windowsengine.FieldError
		if errors.As(err, &field) {
			r.Error = "invalid_config"
			r.FieldError = field
		} else {
			switch err.Error() {
			case "revision_conflict", "stop_before_apply", "config_store_failed", "invalid_config", "cleanup_required", "stop_pending", "already_running", "backend_start_failed", "unsupported_command":
				r.Error = err.Error()
			default:
				r.Error = "operation_failed"
			}
		}
	}
	return r
}
