package windowsclient

import (
	"context"
	"errors"
	"quiclab/internal/vpnclient"
)

// A fresh service exposes the editor without constructing any transport.
type unconfiguredBackend struct{}

func (unconfiguredBackend) Start(context.Context, vpnclient.Config) error {
	return errors.New("config_required")
}
func (unconfiguredBackend) Stop(context.Context) error { return nil }
func (unconfiguredBackend) RuntimeInfo() vpnclient.RuntimeInfo {
	return vpnclient.RuntimeInfo{Mode: "unconfigured"}
}
