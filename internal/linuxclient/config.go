package linuxclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"quiclab/internal/approuting"
	"quiclab/internal/exitcontrol"
	"regexp"
)

type Config struct {
	ServerControl *exitcontrol.Config   `json:"server_control,omitempty"`
	AppRouting    *approuting.TUNPolicy `json:"app_routing,omitempty"`
	Transport     string                `json:"transport"`
	Endpoint      string                `json:"endpoint"`
	Hostname      string                `json:"hostname"`
	Certificate   string                `json:"certificate"`
	Key           string                `json:"key"`
	CA            string                `json:"ca"`
	ServerName    string                `json:"server_name,omitempty"`
	VerifyName    string                `json:"verify_name,omitempty"`
	DNS           string                `json:"dns,omitempty"`
	HealthDNS     string                `json:"health_dns,omitempty"`
	Targets       []string              `json:"targets"`
	Interface     string                `json:"interface,omitempty"`
	AWGConfig     string                `json:"awg_config,omitempty"`
	VLESSURI      string                `json:"vless_uri,omitempty"`
}

func Decode(raw []byte) (Config, error) {
	var c Config
	if len(raw) > 1<<20 {
		return c, errors.New("configuration too large")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return c, errors.New("invalid configuration JSON")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, errors.New("trailing configuration data")
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if c.ServerControl != nil {
		if err := c.ServerControl.Validate(); err != nil {
			return err
		}
	}
	if c.AppRouting != nil {
		if _, e := approuting.CompileLinuxTUN(*c.AppRouting); e != nil {
			return errors.New("invalid Linux application routing policy")
		}
	}
	fail := errors.New("invalid Linux client configuration: use QUIC/HTTPS/AWG/VLESS, IPv4 endpoint and 1–256 nonoverlapping IPv4 subnets")
	if c.Transport != "quic" && c.Transport != "https" && c.Transport != "awg" && c.Transport != "vless" {
		return fail
	}
	ep, e := netip.ParseAddrPort(c.Endpoint)
	if e != nil || !ep.Addr().Is4() || ep.Port() == 0 || !ep.Addr().IsGlobalUnicast() {
		return fail
	}
	if c.Interface != "" && !regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,15}$`).MatchString(c.Interface) {
		return fail
	}
	if len(c.Targets) < 1 || len(c.Targets) > 256 {
		return fail
	}
	var prefixes []netip.Prefix
	for _, s := range c.Targets {
		p, e := netip.ParsePrefix(s)
		if e != nil || !p.Addr().Is4() || p != p.Masked() || p.Bits() == 0 {
			return fail
		}
		if p.Contains(ep.Addr()) || p.Contains(netip.MustParseAddr("10.254.254.2")) {
			return fail
		}
		for _, s := range []string{"0.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "224.0.0.0/3"} {
			if p.Overlaps(netip.MustParsePrefix(s)) {
				return fail
			}
		}
		for _, other := range prefixes {
			if p.Overlaps(other) {
				return fail
			}
		}
		prefixes = append(prefixes, p)
	}
	if c.HealthDNS != "" {
		a, err := netip.ParseAddr(c.HealthDNS)
		if err != nil || !a.Is4() || !a.IsGlobalUnicast() || (c.Transport != "awg" && c.Transport != "vless") {
			return errors.New("health_dns requires AWG/VLESS and a unicast IPv4 address inside targets")
		}
		covered := false
		for _, p := range prefixes {
			covered = covered || p.Contains(a)
		}
		if !covered {
			return errors.New("health_dns must be inside targets")
		}
	}
	return c.validateTransport()
}
func CheckRoutes(targets []string, raw []byte) error {
	var rows []struct {
		Dst   string `json:"dst"`
		Table any    `json:"table"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return errors.New("cannot inspect routes")
	}
	for _, r := range rows {
		if r.Table != nil && r.Table != "main" && r.Table != float64(254) {
			return errors.New("foreign routing table")
		}
		if r.Dst == "default" || r.Dst == "" {
			continue
		}
		p, e := netip.ParsePrefix(r.Dst)
		if e != nil {
			a, e := netip.ParseAddr(r.Dst)
			if e != nil {
				return errors.New("invalid existing route")
			}
			p = netip.PrefixFrom(a, 32)
		}
		for _, s := range targets {
			t, _ := netip.ParsePrefix(s)
			if p.Overlaps(t) {
				return errors.New("selected subnet overlaps existing route")
			}
		}
	}
	return nil
}
func (c Config) endpointIP() string { host, _, _ := net.SplitHostPort(c.Endpoint); return host }
