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
	for _, t := range s.Targets {
		if t == "1.1.1.1/32" {
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
