// Package awg adapts the upstream AmneziaWG engine without implementing cryptography.
package awg

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

type Config struct {
	Endpoint             string   `json:"endpoint"`
	Address              string   `json:"address"`
	DNS                  string   `json:"dns"`
	Allowed              []string `json:"allowed_ips"`
	IPv6Ignored          bool     `json:"ipv6_ignored"`
	MTU                  int      `json:"mtu"`
	private, public, psk string
	params               map[string]string
	keepalive            string
}

// Parse accepts one IPv4 peer and the legacy Jc/Jmin/Jmax/S1/S2/H1-H4 format.
// Errors never contain field values, which may be private keys.
func Parse(raw string) (*Config, error) {
	if len(raw) > 32768 {
		return nil, fmt.Errorf("AWG config exceeds 32 KiB")
	}
	c := &Config{MTU: 1280, params: map[string]string{}}
	section := ""
	seen := map[string]bool{}
	sections := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if line != "[Interface]" && line != "[Peer]" {
				return nil, fmt.Errorf("unsupported AWG section")
			}
			if sections[line] {
				return nil, fmt.Errorf("exactly one Interface and one Peer supported")
			}
			sections[line] = true
			section = line
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if !ok || section == "" {
			return nil, fmt.Errorf("invalid AWG config line")
		}
		tag := section + k
		if seen[tag] {
			return nil, fmt.Errorf("duplicate AWG field %s", k)
		}
		seen[tag] = true
		switch tag {
		case "[Interface]PrivateKey", "[Peer]PublicKey", "[Peer]PresharedKey":
			b, e := base64.StdEncoding.DecodeString(v)
			if e != nil || len(b) != 32 {
				return nil, fmt.Errorf("invalid %s", k)
			}
			allzero := true
			for _, x := range b {
				allzero = allzero && x == 0
			}
			if allzero {
				return nil, fmt.Errorf("zero %s", k)
			}
			s := hex.EncodeToString(b)
			switch k {
			case "PrivateKey":
				c.private = s
			case "PublicKey":
				c.public = s
			default:
				c.psk = s
			}
		case "[Interface]Address":
			for _, s := range strings.Split(v, ",") {
				s = strings.TrimSpace(s)
				if !strings.Contains(s, "/") {
					s += "/32"
				}
				a, e := netip.ParsePrefix(s)
				if e != nil {
					return nil, fmt.Errorf("invalid Address")
				}
				if a.Addr().Is4() {
					if c.Address != "" {
						return nil, fmt.Errorf("one IPv4 Address supported")
					}
					c.Address = a.Addr().String()
				} else {
					c.IPv6Ignored = true
				}
			}
		case "[Interface]DNS":
			a, e := netip.ParseAddr(v)
			if e != nil || !a.Is4() {
				return nil, fmt.Errorf("DNS must be one IPv4 address")
			}
			c.DNS = a.String()
		case "[Interface]MTU":
			n, e := strconv.Atoi(v)
			if e != nil || n < 1280 || n > 1420 {
				return nil, fmt.Errorf("MTU must be 1280..1420")
			}
			c.MTU = n
		case "[Peer]Endpoint":
			h, p, e := net.SplitHostPort(v)
			n, ne := strconv.Atoi(p)
			if e != nil || ne != nil || h == "" || strings.ContainsAny(h, " \r\n\t") || n < 1 || n > 65535 {
				return nil, fmt.Errorf("invalid Endpoint")
			}
			if a, e := netip.ParseAddr(h); e == nil && !a.Is4() {
				return nil, fmt.Errorf("IPv4 Endpoint required")
			}
			c.Endpoint = v
		case "[Peer]AllowedIPs":
			for _, s := range strings.Split(v, ",") {
				a, e := netip.ParsePrefix(strings.TrimSpace(s))
				if e != nil {
					return nil, fmt.Errorf("invalid AllowedIPs")
				}
				if a.Addr().Is4() {
					c.Allowed = append(c.Allowed, a.Masked().String())
				} else {
					c.IPv6Ignored = true
				}
			}
		case "[Peer]PersistentKeepalive":
			n, e := strconv.ParseUint(v, 10, 16)
			if e != nil {
				return nil, fmt.Errorf("invalid PersistentKeepalive")
			}
			c.keepalive = strconv.FormatUint(n, 10)
		default:
			allowed := section == "[Interface]" && (k == "Jc" || k == "Jmin" || k == "Jmax" || k == "S1" || k == "S2" || k == "H1" || k == "H2" || k == "H3" || k == "H4")
			if !allowed {
				return nil, fmt.Errorf("unsupported AWG field %s (legacy format only)", k)
			}
			n, e := strconv.ParseUint(v, 10, 32)
			if e != nil {
				return nil, fmt.Errorf("invalid %s", k)
			}
			if (k == "Jc" && n > 128) || ((k == "Jmin" || k == "Jmax" || k == "S1" || k == "S2") && n > 1280) {
				return nil, fmt.Errorf("%s exceeds supported bound", k)
			}
			c.params[strings.ToLower(k)] = strconv.FormatUint(n, 10)
		}
	}
	if c.private == "" || c.public == "" || c.Address == "" || c.Endpoint == "" || len(c.Allowed) == 0 {
		return nil, fmt.Errorf("AWG requires private/public keys, IPv4 Address, Endpoint and AllowedIPs")
	}
	if c.DNS == "" {
		return nil, fmt.Errorf("AWG requires an IPv4 DNS server for tunnel diagnostics")
	}
	min, _ := strconv.Atoi(c.params["jmin"])
	max, _ := strconv.Atoi(c.params["jmax"])
	if min > max {
		return nil, fmt.Errorf("Jmin exceeds Jmax")
	}
	return c, nil
}
func (c *Config) Metadata() string { b, _ := json.Marshal(c); return string(b) }
func (c *Config) IPC(endpoint string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\n", c.private)
	for _, k := range []string{"jc", "jmin", "jmax", "s1", "s2", "h1", "h2", "h3", "h4"} {
		if v, ok := c.params[k]; ok {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
	}
	fmt.Fprintf(&b, "replace_peers=true\npublic_key=%s\nendpoint=%s\n", c.public, endpoint)
	if c.psk != "" {
		fmt.Fprintf(&b, "preshared_key=%s\n", c.psk)
	}
	if c.keepalive != "" {
		fmt.Fprintf(&b, "persistent_keepalive_interval=%s\n", c.keepalive)
	}
	for _, p := range c.Allowed {
		fmt.Fprintf(&b, "allowed_ip=%s\n", p)
	}
	return b.String()
}
