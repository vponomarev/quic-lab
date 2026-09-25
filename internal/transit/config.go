// Package transit binds VPN egress to a fail-closed Linux AWG uplink.
package transit

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"
)

type Config struct {
	SourceIP    string `json:"source_ip"`
	Endpoint    string `json:"endpoint"`
	DNS         string `json:"dns"`
	ProbeSocket string `json:"probe_socket"`
}

func (c Config) Validate() error {
	for _, v := range []string{c.SourceIP, c.DNS} {
		a, e := netip.ParseAddr(v)
		if e != nil || !a.Is4() || a.IsUnspecified() {
			return errors.New("transit requires IPv4 source and DNS")
		}
	}
	ep, e := netip.ParseAddrPort(c.Endpoint)
	if e != nil || !ep.Addr().Is4() || ep.Port() == 0 {
		return errors.New("transit endpoint must be numeric IPv4:port")
	}
	if c.ProbeSocket == "" {
		return errors.New("transit probe socket required")
	}
	return nil
}
func (c Config) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var local net.Addr
	switch network {
	case "tcp", "tcp4":
		local = &net.TCPAddr{IP: net.ParseIP(c.SourceIP)}
		network = "tcp4"
	case "udp", "udp4":
		local = &net.UDPAddr{IP: net.ParseIP(c.SourceIP)}
		network = "udp4"
	default:
		return nil, errors.New("unsupported transit network")
	}
	return (&net.Dialer{LocalAddr: local}).DialContext(ctx, network, address)
}
func (c Config) Resolver() *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return c.DialContext(ctx, network, net.JoinHostPort(c.DNS, "53"))
	}}
}
func (c Config) Probe(ctx context.Context) error {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", c.ProbeSocket)
	}, DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	client := http.Client{Transport: tr, Timeout: 2 * time.Second}
	req, e := http.NewRequestWithContext(ctx, "GET", "http://transit/probe", nil)
	if e != nil {
		return e
	}
	response, e := client.Do(req)
	if e != nil {
		return errors.New("transit gateway probe unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 204 {
		return errors.New("transit gateway did not respond")
	}
	return nil
}
