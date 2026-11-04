package awg

import (
	"encoding/binary"
	"net/netip"
	"sort"
)

// CoversIPv4 checks the union, including adjacent prefixes. Invalid input fails closed.
// uint64 endpoints permit the exclusive upper bound 2^32 without overflow.
func CoversIPv4(allowed []string, target string) bool {
	type interval struct{ start, end uint64 }
	parse := func(raw string) (interval, bool) {
		p, e := netip.ParsePrefix(raw)
		if e != nil || !p.Addr().Is4() || p != p.Masked() {
			return interval{}, false
		}
		a := p.Addr().As4()
		start := uint64(binary.BigEndian.Uint32(a[:]))
		return interval{start, start + (uint64(1) << uint(32-p.Bits()))}, true
	}
	goal, ok := parse(target)
	if !ok || len(allowed) == 0 || len(allowed) > 8192 {
		return false
	}
	ranges := make([]interval, 0, len(allowed))
	for _, raw := range allowed {
		r, ok := parse(raw)
		if !ok {
			return false
		}
		ranges = append(ranges, r)
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	cursor := goal.start
	for _, r := range ranges {
		if r.end <= cursor {
			continue
		}
		if r.start > cursor {
			return false
		}
		cursor = r.end
		if cursor >= goal.end {
			return true
		}
	}
	return false
}
