package windowsclient

import (
	"context"
	"errors"
	"quiclab/internal/exitcontrol"
	"quiclab/internal/windowsengine"
)

func NewSettingsHandler(m *SettingsManager) *Handler { return &Handler{settings: m} }
func (h *Handler) handleSettings(ctx context.Context, q Request) Response {
	r := Response{Version: 1, ID: q.ID, Capabilities: []string{"settings-v1", "app-routing-tun-v2", "app-routing-live-v1", "profile-library-v1", "server-exits-v1"}}
	var err error
	switch q.Command {
	case "list_server_exits", "select_server_exit":
		var v exitcontrol.View
		v, err = h.settings.ServerExits(ctx, *q.ServerExit)
		if err == nil {
			r.ServerExit = &v
		}
	case "list_profiles":
		v := h.settings.Profiles()
		r.Profiles = &v
	case "save_profile":
		err = h.settings.SaveProfile(ctx, *q.Profile)
	case "delete_profile":
		err = h.settings.DeleteProfile(ctx, *q.Profile)
	case "activate_profile":
		err = h.settings.ActivateProfile(ctx, *q.Profile)
	case "status":
	case "preview_import":
		var meta windowsengine.TransportImport
		meta, err = windowsengine.ImportTransport(q.Import.Transport, q.Import.Content)
		if err == nil {
			r.Import = &meta
		} else {
			err = errors.New("invalid_config")
		}
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
		switch {
		case errors.Is(err, exitcontrol.ErrConflict):
			r.Error = "exit_revision_conflict"
		case errors.Is(err, exitcontrol.ErrDenied):
			r.Error = "exit_denied"
		case errors.Is(err, exitcontrol.ErrAuth):
			r.Error = "exit_auth_expired"
		case errors.Is(err, exitcontrol.ErrUnsupported):
			r.Error = "exit_unsupported"
		case errors.Is(err, exitcontrol.ErrUnavailable):
			r.Error = "exit_unavailable"
		}
		if r.Error != "" {
			return r
		}
		var field *windowsengine.FieldError
		if errors.As(err, &field) {
			r.Error = "invalid_config"
			r.FieldError = field
		} else {
			switch err.Error() {
			case "config_required", "revision_conflict", "stop_before_apply", "config_store_failed", "invalid_config", "cleanup_required", "stop_pending", "already_running", "backend_start_failed", "unsupported_command", "profile_not_found", "profile_limit":
				r.Error = err.Error()
			default:
				r.Error = "operation_failed"
			}
		}
	}
	return r
}
