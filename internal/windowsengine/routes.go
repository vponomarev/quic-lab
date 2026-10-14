package windowsengine

import (
	"errors"
	"net/netip"
	"strings"
)

type RouteSelection struct {
	Target         string `json:"target"`
	InterfaceIndex uint32 `json:"interface_index"`
}

func VerifyRoutes(owner uint32, targets []string, routes []RouteSelection) error {
	if owner == 0 || len(routes) != len(targets) {
		return errors.New("route selection incomplete")
	}
	seen := map[string]bool{}
	for _, r := range routes {
		if r.InterfaceIndex != owner || seen[r.Target] {
			return errors.New("target route bypasses owned adapter")
		}
		seen[r.Target] = true
	}
	for _, t := range targets {
		if !seen[t] {
			return errors.New("missing target route")
		}
	}
	return nil
}
func (s Settings) ValidateProbe(expected string) error {
	a, e := netip.ParseAddr(expected)
	if e != nil || !a.Is4() || !a.IsGlobalUnicast() {
		return errors.New("explicit expected exit IPv4 required")
	}
	if s.FullIPv4() {
		return nil
	}
	for _, t := range s.Targets {
		if p, err := netip.ParsePrefix(t); err == nil && p.Contains(netip.MustParseAddr("1.1.1.1")) {
			return nil
		}
	}
	return errors.New("probe destination must be routed")
}

func VerifyProbeExit(body, expected string) error {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "ip=") {
			if strings.TrimPrefix(line, "ip=") == expected {
				return nil
			}
			return errors.New("unexpected exit IPv4")
		}
	}
	return errors.New("exit IPv4 absent")
}

func validateTargets(targets []string, endpoint netip.Addr) error {
	var selected []netip.Prefix
	reserved := []string{"0.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "224.0.0.0/4", "240.0.0.0/4", "10.254.253.1/32"}
	for row, target := range targets {
		p, err := netip.ParsePrefix(target)
		if err != nil || !p.Addr().Is4() || p.Bits() == 0 || p != p.Masked() || p.Contains(endpoint) {
			return &FieldError{Field: "targets", Row: row + 1, Code: "invalid_ipv4_or_endpoint_overlap"}
		}
		for _, block := range reserved {
			if p.Overlaps(netip.MustParsePrefix(block)) {
				return &FieldError{Field: "targets", Row: row + 1, Code: "reserved_address"}
			}
		}
		for _, previous := range selected {
			if p.Overlaps(previous) {
				return &FieldError{Field: "targets", Row: row + 1, Code: "overlapping_subnet"}
			}
		}
		selected = append(selected, p)
	}
	return nil
}

// VerifyRouteTable checks the entire requested prefix, including more-specific
// foreign routes. Equal-prefix competitors are rejected regardless of metrics.
// This validates the current snapshot; it is not a persistent firewall guard.
func VerifyRouteTable(owner uint32, targets []string, routes []RouteSelection) error {
	if owner == 0 || len(targets) == 0 {
		return errors.New("route table incomplete")
	}
	for _, target := range targets {
		p, err := netip.ParsePrefix(target)
		if err != nil || !p.Addr().Is4() || p != p.Masked() {
			return errors.New("invalid target")
		}
		found := false
		for _, route := range routes {
			r, err := netip.ParsePrefix(route.Target)
			if err != nil || !r.Addr().Is4() {
				return errors.New("invalid route table")
			}
			if r == p && route.InterfaceIndex == owner {
				found = true
			}
			if r.Bits() >= p.Bits() && p.Overlaps(r) && route.InterfaceIndex != owner {
				return errors.New("subnet overlaps a competing route")
			}
		}
		if !found {
			return errors.New("missing owned subnet route")
		}
	}
	return nil
}
