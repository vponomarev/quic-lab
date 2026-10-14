package windowsengine

import (
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"quiclab/internal/awg"
	"quiclab/internal/vless"
	"strings"
)

// Import metadata contains no credentials; imported material is stored in Settings.Key.
type TransportImport struct {
	Name             string   `json:"name"`
	Endpoint         string   `json:"endpoint"`
	SuggestedTargets []string `json:"suggested_targets,omitempty"`
	Notice           string   `json:"notice"`
}

func ImportTransport(kind, raw string) (TransportImport, error) {
	switch kind {
	case "awg":
		c, e := awg.Parse(raw)
		if e != nil {
			return TransportImport{}, errors.New("invalid or unsupported AWG config")
		}
		return TransportImport{Endpoint: c.Endpoint, SuggestedTargets: c.Allowed, Notice: "AWG legacy Jc/Jmin/Jmax/S1/S2/H1–H4. DNS не меняет настройки Windows; AllowedIPs требуют подтверждения."}, nil
	case "vless":
		if !strings.HasPrefix(strings.TrimSpace(raw), "vless://") {
			return TransportImport{}, errors.New("VLESS URI required")
		}
		p, e := vless.ParseImport(raw)
		if e != nil {
			return TransportImport{}, errors.New("invalid or unsupported VLESS URI")
		}
		return TransportImport{Name: p.Name, Endpoint: p.Config.Endpoint, Notice: "VLESS TCP + TLS/REALITY. Список подсетей сохраняется отдельно. ICMP не поддерживается."}, nil
	default:
		return TransportImport{}, errors.New("unsupported transport import")
	}
}
func validEndpointHost(h string) bool {
	if ip := net.ParseIP(h); ip != nil {
		return ip.To4() != nil
	}
	if len(h) == 0 || len(h) > 253 {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func (s Settings) GatewayJSON() string {
	if s.Transport == "awg" {
		b, _ := json.Marshal(map[string]any{"transport": "awg", "endpoint": s.Endpoint, "awg_config": s.Key})
		return string(b)
	}
	if s.Transport == "vless" {
		p, e := vless.ParseImport(s.Key)
		if e != nil {
			return "{}"
		}
		raw, _ := json.Marshal(p)
		b, _ := json.Marshal(map[string]any{"transport": "vless", "endpoint": s.Endpoint, "vless_config": string(raw), "ca": s.CA})
		return string(b)
	}
	b, _ := json.Marshal(s)
	return string(b)
}

func (s Settings) validateTransportOptions() error {
	if s.Transport == "awg" {
		c, e := awg.Parse(s.Key)
		if e != nil {
			return e
		}
		if s.FullIPv4() {
			missing, e := subtractAll(c.Allowed)
			if e != nil || len(missing) != 0 {
				return &FieldError{Field: "routing_mode", Code: "awg_requires_full_allowed_ips"}
			}
			return nil
		}
		for i, target := range s.Targets {
			p, e := netip.ParsePrefix(target)
			if e != nil {
				return e
			}
			covered := false
			for _, allowed := range c.Allowed {
				a := netip.MustParsePrefix(allowed)
				covered = covered || (a.Bits() <= p.Bits() && a.Contains(p.Addr()))
			}
			if !covered {
				return &FieldError{Field: "targets", Row: i + 1, Code: "outside_allowed_ips"}
			}
		}
	}
	if s.Transport == "vless" {
		p, e := vless.ParseImport(s.Key)
		if e != nil {
			return e
		}
		p.Config.RootPEM = []byte(s.CA)
		if vless.ValidateConfig(p.Config) != nil {
			return &FieldError{Field: "ca", Code: "invalid_transport_trust"}
		}
	}
	return nil
}
