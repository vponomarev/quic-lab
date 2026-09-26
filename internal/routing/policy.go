// Package routing implements immutable, ordered VPN profile rules.
package routing

import (
	"encoding/json"
	"fmt"
	"net/netip"
)

type Rule struct {
	ID       string   `json:"id"`
	Mode     string   `json:"mode"`
	UIDs     []int64  `json:"uids,omitempty"`
	Subnets  []string `json:"subnets,omitempty"`
	prefixes []netip.Prefix
}
type Policy struct{ Rules []Rule }

// Unknown ownership must not turn an app rule into an accidental direct bypass.
// DNS is routed explicitly because Android's resolver can own sockets on behalf of apps.
type Decision struct{ Profile, Reason string }

func Parse(raw string) (*Policy, error) {
	var rules []Rule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, err
	}
	if len(rules) == 0 || len(rules) > 32 {
		return nil, fmt.Errorf("choose 1–32 profiles")
	}
	seen := map[string]bool{}
	for i := range rules {
		r := &rules[i]
		if r.ID == "" || r.ID == "direct" || seen[r.ID] {
			return nil, fmt.Errorf("invalid or duplicate profile")
		}
		seen[r.ID] = true
		switch r.Mode {
		case "all":
		case "apps", "exclude":
			if r.Mode == "apps" && len(r.UIDs) == 0 {
				return nil, fmt.Errorf("profile %s: no apps", r.ID)
			}
			for _, uid := range r.UIDs {
				if uid < 0 {
					return nil, fmt.Errorf("invalid UID")
				}
			}
		case "subnets":
			if len(r.Subnets) == 0 {
				return nil, fmt.Errorf("profile %s: no subnets", r.ID)
			}
			for _, s := range r.Subnets {
				v, e := netip.ParsePrefix(s)
				if e != nil || !v.Addr().Is4() {
					return nil, fmt.Errorf("invalid IPv4 subnet %q", s)
				}
				r.prefixes = append(r.prefixes, v.Masked())
			}
		default:
			return nil, fmt.Errorf("invalid rule mode")
		}
	}
	return &Policy{Rules: rules}, nil
}
func (p *Policy) Choose(dst netip.Addr, uid int64) Decision {
	for _, r := range p.Rules {
		switch r.Mode {
		case "all":
			return Decision{r.ID, "all"}
		case "subnets":
			for _, prefix := range r.prefixes {
				if prefix.Contains(dst) {
					return Decision{r.ID, "subnet"}
				}
			}
		case "apps", "exclude":
			if uid < 0 {
				return Decision{"", "unknown_uid"}
			}
			match := false
			for _, u := range r.UIDs {
				if u == uid {
					match = true
					break
				}
			}
			if (r.Mode == "apps" && match) || (r.Mode == "exclude" && !match) {
				return Decision{r.ID, r.Mode}
			}
		}
	}
	return Decision{"direct", "unmatched"}
}
