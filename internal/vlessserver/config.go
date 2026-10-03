// Package vlessserver owns private configuration and lifecycle of the VLESS worker.
package vlessserver

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"quiclab/internal/vless"
	"strconv"
	"strings"
)

type Config struct {
	Listen             string   `json:"listen"`
	Endpoint           string   `json:"endpoint"`
	Security           string   `json:"security"`
	ServerName         string   `json:"server_name"`
	Fingerprint        string   `json:"fingerprint,omitempty"`
	Flow               string   `json:"flow,omitempty"`
	TLSCertificateFile string   `json:"tls_certificate_file,omitempty"`
	TLSKeyFile         string   `json:"tls_key_file,omitempty"`
	RealityTarget      string   `json:"reality_target,omitempty"`
	RealityServerNames []string `json:"reality_server_names,omitempty"`
	RealityPrivateKey  string   `json:"reality_private_key,omitempty"`
	RealityShortIDs    []string `json:"reality_short_ids,omitempty"`
	Mode               string   `json:"mode"`
	DemuxEndpoint      string   `json:"demux_endpoint,omitempty"`
}

var errConfig = errors.New("invalid VLESS server configuration")

func endpoint(value string, listen bool) bool {
	h, p, e := net.SplitHostPort(value)
	if e != nil || h == "" || len(h) > 253 || strings.ContainsAny(h, " /\\\r\n\t@?#%") {
		return false
	}
	n, e := strconv.ParseUint(p, 10, 16)
	if e != nil || n == 0 {
		return false
	}
	ip, e := netip.ParseAddr(h)
	if listen {
		return e == nil && ip.Is4() && !ip.IsMulticast()
	}
	if e == nil {
		return ip.Is4() && !ip.IsMulticast() && !ip.IsUnspecified()
	}
	if strings.Contains(h, ":") {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
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
func (c Config) publicKey() (string, error) {
	raw, e := base64.RawURLEncoding.Strict().DecodeString(c.RealityPrivateKey)
	if e != nil || len(raw) != 32 || c.RealityPrivateKey == base64.RawURLEncoding.EncodeToString(make([]byte, 32)) {
		return "", errConfig
	}
	k, e := ecdh.X25519().NewPrivateKey(raw)
	if e != nil {
		return "", errConfig
	}
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}
func (c Config) Validate() error {
	if !endpoint(c.Listen, true) || !endpoint(c.Endpoint, false) {
		return errConfig
	}
	switch c.Mode {
	case "standalone":
		if c.DemuxEndpoint != "" {
			return errConfig
		}
	case "demux-only":
		if !endpoint(c.DemuxEndpoint, false) {
			return errConfig
		}
	default:
		return errConfig
	}
	switch c.Security {
	case "tls":
		if !filepath.IsAbs(c.TLSCertificateFile) || !filepath.IsAbs(c.TLSKeyFile) || strings.ContainsAny(c.TLSCertificateFile+c.TLSKeyFile, "\x00\r\n") || c.RealityTarget != "" || c.RealityPrivateKey != "" || len(c.RealityServerNames) > 0 || len(c.RealityShortIDs) > 0 {
			return errConfig
		}
	case "reality":
		if c.TLSCertificateFile != "" || c.TLSKeyFile != "" || !endpoint(c.RealityTarget, false) || len(c.RealityServerNames) == 0 || len(c.RealityServerNames) > 16 || len(c.RealityShortIDs) == 0 || len(c.RealityShortIDs) > 16 {
			return errConfig
		}
		if _, e := c.publicKey(); e != nil {
			return errConfig
		}
		names := map[string]bool{}
		selected := false
		for _, name := range c.RealityServerNames {
			if !endpoint(net.JoinHostPort(name, "443"), false) || names[strings.ToLower(name)] {
				return errConfig
			}
			names[strings.ToLower(name)] = true
			selected = selected || name == c.ServerName
		}
		if !selected {
			return errConfig
		}
		ids := map[[8]byte]bool{}
		for _, id := range c.RealityShortIDs {
			raw, e := hex.DecodeString(id)
			if e != nil || len(raw) > 8 {
				return errConfig
			}
			var padded [8]byte
			copy(padded[:], raw)
			if ids[padded] {
				return errConfig
			}
			ids[padded] = true
		}
	default:
		return errConfig
	}
	// Keep exported profiles within the exact Android compatibility contract.
	raw, e := c.clientURI("11111111-1111-4111-8111-111111111111", "validation")
	if e != nil {
		return errConfig
	}
	if _, e = vless.ParseImport(raw); e != nil {
		return errConfig
	}
	return nil
}
