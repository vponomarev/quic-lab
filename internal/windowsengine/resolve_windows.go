package windowsengine

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"syscall"
)

func (WindowsPlatform) Resolve(ctx context.Context, s Settings) (Settings, error) {
	host, port, e := net.SplitHostPort(s.Endpoint)
	if e != nil {
		return s, e
	}
	if a, e := netip.ParseAddr(host); e == nil && a.Is4() {
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
		if validateTargets(s.Targets, ip) != nil {
			return s, errors.New("resolved endpoint overlaps routes")
		}
	}
	s.Endpoint = net.JoinHostPort(ips[0].String(), port)
	return s, nil
}
