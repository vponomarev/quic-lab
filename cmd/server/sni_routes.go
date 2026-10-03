package main

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

type sniRoute struct {
	ServerNames   []string `json:"server_names"`
	Target        string   `json:"target"`
	ProxyProtocol bool     `json:"proxy_protocol,omitempty"`
}

func exactSNI(name string) bool {
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func validateSNIRoutes(host string, allowed []string, routes []sniRoute) (map[string]sniRoute, error) {
	if !exactSNI(host) || len(routes) > 64 {
		return nil, errors.New("invalid TLS host or too many TLS routes")
	}
	seen := map[string]bool{strings.ToLower(host): true}
	for _, name := range allowed {
		if !exactSNI(name) {
			return nil, errors.New("invalid VPN SNI name")
		}
		seen[strings.ToLower(name)] = true
	}
	result := make(map[string]sniRoute)
	for _, route := range routes {
		if len(route.ServerNames) == 0 || len(route.ServerNames) > 16 {
			return nil, errors.New("TLS route requires 1..16 server names")
		}
		if err := validateAddress(route.Target, true); err != nil {
			return nil, fmt.Errorf("TLS route target: %w", err)
		}
		if route.ProxyProtocol {
			h, _, _ := net.SplitHostPort(route.Target)
			ip := net.ParseIP(h)
			if ip == nil || !ip.IsLoopback() {
				return nil, errors.New("PROXY protocol target must be a numeric loopback address")
			}
		}
		for _, name := range route.ServerNames {
			name = strings.ToLower(name)
			if !exactSNI(name) || seen[name] {
				return nil, errors.New("invalid or conflicting TLS route server name")
			}
			seen[name] = true
			result[name] = route
		}
	}
	return result, nil
}

func routeTargetsListener(target, listen string) bool {
	th, tp, te := net.SplitHostPort(target)
	lh, lp, le := net.SplitHostPort(listen)
	if te != nil || le != nil || tp != lp {
		return false
	}
	if strings.EqualFold(th, lh) {
		return true
	}
	tip, lip := net.ParseIP(th), net.ParseIP(lh)
	// Wildcard sockets own local interfaces, not every remote host on this port.
	if lh == "" || lip != nil && lip.IsUnspecified() {
		if strings.EqualFold(th, "localhost") || tip != nil && tip.IsLoopback() {
			return true
		}
		if tip != nil {
			addresses, err := net.InterfaceAddrs()
			if err == nil {
				for _, address := range addresses {
					ip, _, parseErr := net.ParseCIDR(address.String())
					if parseErr == nil && ip.Equal(tip) {
						return true
					}
				}
			}
		}
		return false
	}
	return tip != nil && lip != nil && tip.Equal(lip)
}
