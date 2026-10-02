package routing

import (
	"fmt"
	"net/netip"
)

type DNSPolicy struct {
	Mode    string       `json:"mode"`
	ExitID  string       `json:"exit_id,omitempty"`
	Servers []netip.Addr `json:"servers,omitempty"`
	Network string       `json:"network,omitempty"`
}

func ValidateDNS(p DNSPolicy, exits []string) error {
	switch p.Mode {
	case "tunnel":
		for _, id := range exits {
			if id == p.ExitID {
				return nil
			}
		}
		return fmt.Errorf("choose a DNS exit")
	case "system":
		if p.ExitID != "" {
			return fmt.Errorf("system DNS cannot select an exit")
		}
		for _, ip := range p.Servers {
			if !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() {
				return fmt.Errorf("DNS requires unicast IPv4")
			}
		}
		if p.Network != "" && p.Network != "wifi" && p.Network != "cell" {
			return fmt.Errorf("invalid DNS network")
		}
		return nil
	default:
		return fmt.Errorf("DNS mode must be tunnel or system")
	}
}
