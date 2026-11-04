package linuxclient

import (
	"encoding/json"
	"errors"
	"net/netip"
	"quiclab/internal/awg"
	"quiclab/internal/vless"
	"strings"
)

func (c Config) validateTransport() error {
	fail := errors.New("invalid or incompatible transport profile")
	if c.DNS != "" {
		a, e := netip.ParseAddr(c.DNS)
		if e != nil || !a.Is4() || !a.IsGlobalUnicast() {
			return fail
		}
	}
	switch c.Transport {
	case "quic", "https":
		if c.AWGConfig != "" || c.VLESSURI != "" {
			return fail
		}
	case "awg":
		if c.VLESSURI != "" || c.Key != "" || c.Certificate != "" || c.Hostname != "" || c.CA != "" || c.ServerName != "" || c.VerifyName != "" {
			return fail
		}
		profile, e := awg.Parse(c.AWGConfig)
		if e != nil {
			return fail
		}
		for _, target := range c.Targets {
			if !awg.CoversIPv4(profile.Allowed, target) {
				return errors.New("selected subnet is outside AWG AllowedIPs")
			}
		}
	case "vless":
		if c.AWGConfig != "" || c.Key != "" || c.Certificate != "" || c.Hostname != "" || c.ServerName != "" || c.VerifyName != "" || !strings.HasPrefix(strings.TrimSpace(c.VLESSURI), "vless://") {
			return fail
		}
		profile, e := vless.ParseImport(c.VLESSURI)
		if e != nil {
			return fail
		}
		profile.Config.RootPEM = []byte(c.CA)
		if vless.ValidateConfig(profile.Config) != nil {
			return fail
		}
	default:
		return fail
	}
	return nil
}

// GatewayJSON projects validated credentials for the selected transport only.
func (c Config) GatewayJSON() (string, error) {
	if e := c.Validate(); e != nil {
		return "", e
	}
	out := map[string]any{"transport": c.Transport, "endpoint": c.Endpoint}
	switch c.Transport {
	case "awg":
		out["awg_config"] = c.AWGConfig
	case "vless":
		p, _ := vless.ParseImport(c.VLESSURI)
		raw, e := json.Marshal(p)
		if e != nil {
			return "", errors.New("invalid VLESS profile")
		}
		out["vless_config"] = string(raw)
		out["ca"] = c.CA
	default:
		out["hostname"] = c.Hostname
		out["certificate"] = c.Certificate
		out["key"] = c.Key
		out["ca"] = c.CA
		out["server_name"] = c.ServerName
		out["verify_name"] = c.VerifyName
	}
	if c.DNS != "" {
		out["dns"] = c.DNS
	}
	raw, e := json.Marshal(out)
	return string(raw), e
}
