package windowsengine

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"quiclab/internal/vpnclient"
	"quiclab/internal/vpnmodel"
	"reflect"
	"strconv"
)

// Settings supports selected subnets or full IPv4. Missing modes preserve legacy routing.
const MaxTargets = 256

type Settings struct {
	RoutingMode    string   `json:"routing_mode,omitempty"`
	UplinkMode     string   `json:"uplink_mode,omitempty"`
	DNS            string   `json:"dns,omitempty"`
	Name           string   `json:"name,omitempty"`
	Transport      string   `json:"transport"`
	Endpoint       string   `json:"endpoint"`
	Hostname       string   `json:"hostname"`
	Certificate    string   `json:"certificate"`
	Key            string   `json:"key"`
	CA             string   `json:"ca,omitempty"`
	ServerName     string   `json:"server_name,omitempty"`
	VerifyName     string   `json:"verify_name,omitempty"`
	InterfaceIndex uint32   `json:"interface_index"`
	Targets        []string `json:"targets"`
}

func (s Settings) Validate() error {
	if (s.RoutingMode != "" && s.RoutingMode != "subnets" && s.RoutingMode != "all_ipv4") || (s.UplinkMode != "" && s.UplinkMode != "manual" && s.UplinkMode != "auto") {
		return &FieldError{Field: "routing_mode", Code: "invalid_mode"}
	}
	if s.DNS != "" {
		a, e := netip.ParseAddr(s.DNS)
		if e != nil || !a.Is4() || !a.IsGlobalUnicast() {
			return &FieldError{Field: "dns", Code: "invalid_ipv4"}
		}
	}
	if len(s.Name) > 128 {
		return &FieldError{Field: "name", Code: "too_long"}
	}
	host, port, e := net.SplitHostPort(s.Endpoint)
	a, _ := netip.ParseAddr(host)
	pn, pe := strconv.Atoi(port)
	if e != nil || !validEndpointHost(host) || pe != nil || pn < 1 || pn > 65535 || s.Key == "" || (s.InterfaceIndex == 0 && s.UplinkMode != "auto") || s.InterfaceIndex > 0xffffff || (s.Transport != "quic" && s.Transport != "https" && s.Transport != "awg" && s.Transport != "vless") || (len(s.Targets) == 0 && !s.FullIPv4()) || len(s.Targets) > MaxTargets {
		return &FieldError{Field: "connection", Code: "invalid_connection_or_route_count"}
	}
	if s.Transport == "quic" || s.Transport == "https" {
		if s.Hostname == "" || s.Certificate == "" {
			return &FieldError{Field: "connection", Code: "certificate_required"}
		}
	} else {
		if _, e := ImportTransport(s.Transport, s.Key); e != nil {
			return &FieldError{Field: "credentials", Code: "unsupported_or_invalid_profile"}
		}
	}
	if err := s.validateTransportOptions(); err != nil {
		return err
	}
	if !s.FullIPv4() {
		if err := validateTargets(s.Targets, a); err != nil {
			return err
		}

	}
	return nil
}

type Link interface {
	Configure(context.Context, Settings) error
	Close() error
}
type Platform interface {
	Open(context.Context, Settings) (Link, error)
}
type Gateway interface {
	Start(context.Context, Settings) error
	Attach(Link) error
	Stop()
}
type Engine struct {
	s       Settings
	p       Platform
	g       Gateway
	link    Link
	started bool
}

func New(s Settings, p Platform, g Gateway) *Engine {
	s.Targets = append([]string(nil), s.Targets...)
	return &Engine{s: s, p: p, g: g}
}
func (e *Engine) Config() vpnclient.Config {
	return vpnclient.Config{Revision: 1, Profiles: vpnmodel.Config{Version: 1, Exits: []vpnmodel.Exit{{ID: "windows", Name: "Windows MVP", Kind: "standalone"}}, Profiles: []vpnmodel.Profile{{ID: "windows", ExitID: "windows", Transport: e.s.Transport, Endpoint: e.s.Endpoint, Mode: "auto", PoolSize: 1}}}}
}
func (e *Engine) Start(ctx context.Context, c vpnclient.Config) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.s.Validate(); err != nil {
		return err
	}
	if !reflect.DeepEqual(c, e.Config()) || e.link != nil {
		return errors.New("configuration mismatch or already running")
	}
	effective := e.s
	if resolver, ok := e.p.(interface {
		Resolve(context.Context, Settings) (Settings, error)
	}); ok {
		var err error
		effective, err = resolver.Resolve(ctx, e.s)
		if err != nil {
			return err
		}
	}
	l, err := e.p.Open(ctx, effective)
	if err != nil {
		return err
	}
	e.link = l
	e.started = true
	if err = e.g.Start(ctx, effective); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = e.g.Attach(l); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return l.Configure(ctx, effective)
}
func (e *Engine) Stop(ctx context.Context) error {
	// Stop input before shutting down the stack: late TCP handshakes otherwise
	// register after Stack.Close and leave Stack.Wait blocked.
	var linkErr error
	if e.link != nil {
		linkErr = e.link.Close()
	}
	if e.started {
		e.g.Stop()
		e.started = false
	}
	if linkErr != nil {
		return linkErr
	}
	e.link = nil
	return nil
}

func (e *Engine) RuntimeInfo() vpnclient.RuntimeInfo {
	info := vpnclient.RuntimeInfo{RoutingMode: e.s.RoutingMode, Name: e.s.Name, Mode: "vpn", Transport: e.s.Transport, Endpoint: e.s.Endpoint, RouteCount: len(e.s.Targets), Targets: append([]string(nil), e.s.Targets...)}
	if p, ok := e.g.(interface{ TrafficSnapshot() vpnclient.Traffic }); ok {
		info.Traffic = p.TrafficSnapshot()
	}
	return info
}

func (e *Engine) ValidateConfig(c vpnclient.Config) error {
	if e.s.Validate() != nil || !reflect.DeepEqual(c, e.Config()) {
		return vpnclient.ErrInvalidConfig
	}
	return nil
}

// Health is read by the automatic owner, serialized with Start/Stop.
func (e *Engine) Healthy() bool {
	if p, ok := e.g.(interface{ Healthy() bool }); ok {
		return p.Healthy()
	}
	return e.started
}
