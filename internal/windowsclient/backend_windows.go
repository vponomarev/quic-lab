//go:build windows

package windowsclient

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
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
	var b windowsengine.ManagedEngine
	if s.UplinkMode == "auto" {
		b = windowsengine.NewWindowsAutomatic(s)
	} else {
		b = windowsengine.New(s, windowsengine.WindowsPlatform{}, &windowsengine.NativeGateway{})
	}
	c := b.Config()
	return b, &c, nil
}

// Construction has no network effects; credential syntax is checked before Apply stops VPN.
func newSettingsBackend(s windowsengine.Settings) (vpnclient.Backend, vpnclient.Config, error) {
	if e := s.Validate(); e != nil {
		return nil, vpnclient.Config{}, e
	}
	if s.Transport == "quic" || s.Transport == "https" {
		if _, e := tls.X509KeyPair([]byte(s.Certificate), []byte(s.Key)); e != nil {
			return nil, vpnclient.Config{}, errors.New("invalid client certificate or key")
		}
		if s.CA != "" && !x509.NewCertPool().AppendCertsFromPEM([]byte(s.CA)) {
			return nil, vpnclient.Config{}, errors.New("invalid trust root")
		}
	}
	var b windowsengine.ManagedEngine
	if s.UplinkMode == "auto" {
		b = windowsengine.NewWindowsAutomatic(s)
	} else {
		b = windowsengine.New(s, windowsengine.WindowsPlatform{}, &windowsengine.NativeGateway{})
	}
	return b, b.Config(), nil
}
