package servertls

import (
	"crypto/tls"
	"errors"
	"strings"
)

// Empty cover lists preserve legacy listeners; configured lists are exact names.
func ServerNameAllowed(realName string, covers []string, name string) bool {
	if len(covers) == 0 {
		return true
	}
	if name == "" {
		return false
	}
	if strings.EqualFold(name, realName) {
		return true
	}
	for _, cover := range covers {
		if strings.EqualFold(name, cover) {
			return true
		}
	}
	return false
}

// GetConfigForClient applies to every ClientHello, including resumed sessions.
func AllowServerNames(cfg *tls.Config, realName string, covers []string) *tls.Config {
	result := cfg.Clone()
	if len(covers) == 0 {
		return result
	}
	names := append([]string(nil), covers...)
	previous := cfg.GetConfigForClient
	result.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		if !ServerNameAllowed(realName, names, hello.ServerName) {
			return nil, errors.New("VPN SNI not allowed")
		}
		if previous != nil {
			return previous(hello)
		}
		return nil, nil
	}
	return result
}
