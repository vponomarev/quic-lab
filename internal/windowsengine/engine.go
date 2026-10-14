package windowsengine

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"quiclab/internal/vpnclient"
	"quiclab/internal/vpnmodel"
	"reflect"
	"strconv"
)

// Settings supports explicit IPv4 subnets. Default routes and IPv6 are refused.
type Settings struct {
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
	host, port, e := net.SplitHostPort(s.Endpoint)
	a, ae := netip.ParseAddr(host)
	pn, pe := strconv.Atoi(port)
	if e != nil || ae != nil || !a.Is4() || pe != nil || pn < 1 || pn > 65535 || s.Hostname == "" || s.Certificate == "" || s.Key == "" || s.InterfaceIndex == 0 || s.InterfaceIndex > 0xffffff || (s.Transport != "quic" && s.Transport != "https") || len(s.Targets) == 0 || len(s.Targets) > 16 {
		return errors.New("invalid bounded engine settings")
	}
	if err := validateTargets(s.Targets, a); err != nil {
		return err
	}

	return nil
}
func (s Settings) GatewayJSON() string { b, _ := json.Marshal(s); return string(b) }

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
	l, err := e.p.Open(ctx, e.s)
	if err != nil {
		return err
	}
	e.link = l
	e.started = true
	if err = e.g.Start(ctx, e.s); err != nil {
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
	return l.Configure(ctx, e.s)
}
func (e *Engine) Stop(ctx context.Context) error {
	if e.started {
		e.g.Stop()
		e.started = false
	}
	if e.link != nil {
		if err := e.link.Close(); err != nil {
			return err
		}
		e.link = nil
	}
	return nil
}

func (e *Engine) RuntimeInfo() vpnclient.RuntimeInfo {
	info := vpnclient.RuntimeInfo{Mode: "vpn", Transport: e.s.Transport, Endpoint: e.s.Endpoint, RouteCount: len(e.s.Targets), Targets: append([]string(nil), e.s.Targets...)}
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
