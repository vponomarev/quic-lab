//go:build windows

package windowsclient

import (
	"quiclab/internal/vpnclient"
	"quiclab/internal/windowsengine"
)

// No network effects until Controller.Start. The real profile is administrator-provisioned in private state.
func selectServiceBackend(raw []byte) (vpnclient.Backend, *vpnclient.Config, error) {
	if raw == nil {
		return FixtureBackend{}, nil, nil
	}
	s, e := windowsengine.ParseSettings(raw)
	if e != nil {
		return nil, nil, e
	}
	b := windowsengine.New(s, windowsengine.WindowsPlatform{}, &windowsengine.NativeGateway{})
	c := b.Config()
	return b, &c, nil
}
