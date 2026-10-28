package windowsengine

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"syscall"
)

func (WindowsPlatform) Resolve(ctx context.Context, s Settings) (Settings, error) {
	if err := ctx.Err(); err != nil {
		return s, err
	}
	var u Uplink
	if s.FullIPv4() || s.UplinkMode == "auto" {
		var ok bool
		u, ok = ctx.Value(uplinkSnapshotKey{}).(Uplink)
		if !ok || u.Index == 0 || u.Index != s.InterfaceIndex {
			var e error
			u, e = DiscoverWindowsUplink(ctx, s)
			if e != nil {
				return s, e
			}
		}
		s.InterfaceIndex = u.Index
	}
	host, port, e := net.SplitHostPort(s.Endpoint)
	if e != nil {
		return s, e
	}
	if a, e := netip.ParseAddr(host); e == nil && a.Is4() {
		if s.FullIPv4() {
			s.Targets, e = FullIPv4Routes(a, u.Local)
			if e != nil {
				return s, e
			}
		}
		if s.FullIPv4() && !containsIPv4(s.Targets, s.DNSAddress()) {
			return s, errors.New("DNS server excluded from tunnel")
		}
		return s, nil
	}
	resolver := net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		d := net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
			var bindErr error
			e := raw.Control(func(fd uintptr) { bindErr = interfaceBinder(s.InterfaceIndex).Bind(int64(fd)) })
			if e != nil {
				return e
			}
			return bindErr
		}}
		return d.DialContext(ctx, network, address)
	}}
	ips, e := resolver.LookupNetIP(ctx, "ip4", host)
	if e != nil || len(ips) == 0 {
		return s, errors.New("endpoint resolution failed")
	}
	for _, ip := range ips {
		if !ip.Is4() {
			continue
		}
		if !s.FullIPv4() && validateTargets(s.Targets, ip) != nil {
			return s, errors.New("resolved endpoint overlaps routes")
		}
	}
	s.Endpoint = net.JoinHostPort(ips[0].String(), port)
	if s.FullIPv4() {
		s.Targets, e = FullIPv4Routes(ips[0], u.Local)
		if e != nil {
			return s, e
		}
	}
	if s.FullIPv4() && !containsIPv4(s.Targets, s.DNSAddress()) {
		return s, errors.New("DNS server excluded from tunnel")
	}
	return s, nil
}

func containsIPv4(routes []string, ip string) bool {
	a, e := netip.ParseAddr(ip)
	if e != nil {
		return false
	}
	for _, s := range routes {
		p, e := netip.ParsePrefix(s)
		if e == nil && p.Contains(a) {
			return true
		}
	}
	return false
}
