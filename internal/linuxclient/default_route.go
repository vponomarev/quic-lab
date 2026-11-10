package linuxclient

import (
	"encoding/json"
	"errors"
	"net/netip"
)

// Pure planning: no physical routes are replaced or removed. Call before TUN
// allocation, then install the endpoint exception before two /1 capture routes.
type defaultRoutePlan struct {
	pin []string
}

func planDefaultRoute(endpoint string, main, lookup []byte) (defaultRoutePlan, error) {
	var plan defaultRoutePlan
	var rows []struct {
		Dst string `json:"dst"`
	}
	var paths []struct {
		Dev     string `json:"dev"`
		Gateway string `json:"gateway"`
		Type    string `json:"type"`
	}
	if json.Unmarshal(main, &rows) != nil || json.Unmarshal(lookup, &paths) != nil || len(paths) != 1 || paths[0].Dev == "" || paths[0].Dev == "lo" || (paths[0].Type != "" && paths[0].Type != "unicast") {
		return plan, errors.New("cannot plan physical endpoint route")
	}
	ip, err := netip.ParseAddr(endpoint)
	if err != nil || !ip.Is4() {
		return plan, errors.New("invalid endpoint address")
	}
	protected := false
	for _, row := range rows {
		if row.Dst == "default" || row.Dst == "0.0.0.0/0" || row.Dst == "" {
			continue
		}
		p, err := netip.ParsePrefix(row.Dst)
		if err != nil {
			addr, e := netip.ParseAddr(row.Dst)
			if e != nil {
				return plan, errors.New("invalid existing route")
			}
			p = netip.PrefixFrom(addr, 32)
		}
		if p.Bits() <= 1 {
			return plan, errors.New("full IPv4 conflicts with existing /1 route")
		}
		protected = protected || p.Contains(ip)
	}
	if !protected {
		plan.pin = []string{endpoint + "/32"}
		if gateway := paths[0].Gateway; gateway != "" {
			addr, e := netip.ParseAddr(gateway)
			if e != nil || !addr.Is4() {
				return plan, errors.New("invalid physical gateway")
			}
			plan.pin = append(plan.pin, "via", gateway)
		}
		plan.pin = append(plan.pin, "dev", paths[0].Dev, "proto", "242", "metric", "42760")
		if paths[0].Gateway != "" {
			// route get already resolved this physical nexthop. Preserve its
			// reachability even for /32 underlays: route get omits onlink flags.
			plan.pin = append(plan.pin, "onlink")
		}
	}
	return plan, nil
}

func (p defaultRoutePlan) captureArgs(name string) [][]string {
	return [][]string{
		{"-4", "route", "add", "0.0.0.0/1", "dev", name, "proto", "242"},
		{"-4", "route", "add", "128.0.0.0/1", "dev", name, "proto", "242"},
	}
}
