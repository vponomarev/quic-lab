package main

import (
	"context"
	"errors"
	"net"
	"quiclab/internal/vlessserver"
	"sort"
	"strings"
	"sync"
)

// Only this controller owns the managed VLESS overlay. Published maps are immutable.
type managedRouteController struct {
	mu        sync.RWMutex
	host      string
	allowed   []string
	manual    []sniRoute
	listeners []string
	targets   map[string]sniRoute
	revision  uint64
	routed    bool
}

func managedVLESSRoutes(c vlessserver.Config) []sniRoute {
	if !c.AcceptProxyProtocol {
		return nil
	}
	names := []string{c.ServerName}
	if c.Security == "reality" {
		names = append([]string(nil), c.RealityServerNames...)
	}
	return []sniRoute{{ServerNames: names, Target: c.Listen, ProxyProtocol: true}}
}
func sameRouteNames(a, b []string) bool {
	norm := func(in []string) []string {
		out := make([]string, len(in))
		for i, n := range in {
			out[i] = strings.ToLower(n)
		}
		sort.Strings(out)
		return out
	}
	return strings.Join(norm(a), "\x00") == strings.Join(norm(b), "\x00")
}
func newManagedRouteController(host string, allowed []string, routes []sniRoute, initial vlessserver.Config, listeners []string) (*managedRouteController, error) {
	c := &managedRouteController{host: host, allowed: append([]string(nil), allowed...), listeners: append([]string(nil), listeners...), routed: initial.AcceptProxyProtocol}
	expected := managedVLESSRoutes(initial)
	owned := false
	for _, route := range routes {
		if route.Target == initial.Listen && initial.AcceptProxyProtocol {
			if owned || !route.ProxyProtocol || len(expected) != 1 || (!sameRouteNames(route.ServerNames, expected[0].ServerNames) && !sameRouteNames(route.ServerNames, []string{initial.ServerName})) {
				return nil, errors.New("ambiguous managed VLESS route; reconcile manual configuration")
			}
			owned = true
			continue
		}
		route.ServerNames = append([]string(nil), route.ServerNames...)
		c.manual = append(c.manual, route)
	}
	targets, err := c.build(initial)
	if err != nil {
		return nil, err
	}
	c.targets = targets
	return c, nil
}
func (c *managedRouteController) build(cfg vlessserver.Config) (map[string]sniRoute, error) {
	if cfg.AcceptProxyProtocol != c.routed {
		return nil, errors.New("changing common-port mode requires installer configuration")
	}
	if c.routed {
		h, _, err := net.SplitHostPort(cfg.Listen)
		if err != nil || net.ParseIP(h) == nil || !net.ParseIP(h).IsLoopback() {
			return nil, errors.New("managed backend requires numeric loopback")
		}
	}
	for _, listen := range c.listeners {
		if routeTargetsListener(cfg.Listen, listen) {
			return nil, errors.New("VLESS backend conflicts with a server listener")
		}
	}
	routes := append(append([]sniRoute(nil), c.manual...), managedVLESSRoutes(cfg)...)
	return validateSNIRoutes(c.host, c.allowed, routes)
}
func (c *managedRouteController) Validate(cfg vlessserver.Config) error {
	_, err := c.build(cfg)
	return err
}
func (c *managedRouteController) Apply(ctx context.Context, cfg vlessserver.Config, revision uint64) error {
	targets, err := c.build(cfg)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return err
	}
	if revision < c.revision {
		return errors.New("stale managed route revision")
	}
	c.targets = targets
	c.revision = revision
	return nil
}
func (c *managedRouteController) snapshot() map[string]sniRoute {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.targets
}
