package windowsengine

import (
	"encoding/binary"
	"errors"
	"net/netip"
)

var ipv4Reserved = []string{"0.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "224.0.0.0/4", "240.0.0.0/4", "10.254.253.1/32"}

func subtractPrefix(p, exclude netip.Prefix) []netip.Prefix {
	if !p.Overlaps(exclude) {
		return []netip.Prefix{p}
	}
	if exclude.Bits() <= p.Bits() {
		return nil
	}
	bits := p.Bits() + 1
	addr := p.Addr().As4()
	right := binary.BigEndian.Uint32(addr[:]) | (uint32(1) << uint(32-bits))
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], right)
	out := subtractPrefix(netip.PrefixFrom(p.Addr(), bits), exclude)
	return append(out, subtractPrefix(netip.PrefixFrom(netip.AddrFrom4(b), bits), exclude)...)
}
func subtractAll(exclusions []string) ([]string, error) {
	prefixes := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}
	for _, raw := range exclusions {
		p, e := netip.ParsePrefix(raw)
		if e != nil || !p.Addr().Is4() || p != p.Masked() {
			return nil, errors.New("invalid IPv4 exclusion")
		}
		var next []netip.Prefix
		for _, old := range prefixes {
			next = append(next, subtractPrefix(old, p)...)
		}
		prefixes = next
		if len(prefixes) > 2048 {
			return nil, errors.New("too many effective routes")
		}
	}
	out := make([]string, len(prefixes))
	for i, p := range prefixes {
		out[i] = p.String()
	}
	return out, nil
}

// Physical routes remain intact. More-specific owned routes cover all other IPv4 destinations.
func FullIPv4Routes(endpoint netip.Addr, local []string) ([]string, error) {
	if !endpoint.Is4() || len(local) > 256 {
		return nil, errors.New("invalid external network")
	}
	exclusions := append([]string(nil), ipv4Reserved...)
	exclusions = append(exclusions, endpoint.String()+"/32")
	for _, raw := range local {
		p, e := netip.ParsePrefix(raw)
		if e != nil || p.Bits() == 0 {
			return nil, errors.New("invalid on-link network")
		}
		exclusions = append(exclusions, raw)
	}
	return subtractAll(exclusions)
}
func (s Settings) FullIPv4() bool { return s.RoutingMode == "all_ipv4" }
func (s Settings) DNSAddress() string {
	if s.DNS != "" {
		return s.DNS
	}
	return "1.1.1.1"
}
