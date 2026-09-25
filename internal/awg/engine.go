package awg

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"quiclab/internal/awg/netstack"
)

type Binder interface{ Bind(int64) error }
type protectedBind struct {
	conn.Bind
	mu     sync.Mutex
	binder Binder
}

func (b *protectedBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	f, p, e := b.Bind.Open(port)
	if e != nil {
		return nil, 0, e
	}
	if b.binder != nil {
		peek, ok := b.Bind.(conn.PeekLookAtSocketFd)
		if !ok {
			b.Bind.Close()
			return nil, 0, errors.New("socket protection unavailable")
		}
		fd, e := peek.PeekLookAtSocketFd4()
		if e == nil {
			e = b.binder.Bind(int64(fd))
		}
		if e != nil {
			b.Bind.Close()
			return nil, 0, e
		}
		// IPv6 transport is unused, but protect its socket too when present.
		if fd, e := peek.PeekLookAtSocketFd6(); e == nil {
			if e = b.binder.Bind(int64(fd)); e != nil {
				b.Bind.Close()
				return nil, 0, e
			}
		}
	}
	return f, p, nil
}

type Engine struct {
	mu      sync.Mutex
	device  *device.Device
	network *netstack.Net
	bind    *protectedBind
	allowed []netip.Prefix
}

func Start(c *Config, endpoint string, binder Binder) (*Engine, error) {
	addr, e := netip.ParseAddrPort(endpoint)
	if e != nil || !addr.Addr().Is4() {
		return nil, errors.New("numeric IPv4 endpoint required")
	}
	tun, n, e := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr(c.Address)}, []netip.Addr{netip.MustParseAddr(c.DNS)}, c.MTU)
	if e != nil {
		return nil, e
	}
	b := &protectedBind{Bind: conn.NewStdNetBind(), binder: binder}
	// UAPI and verbose logs can contain key material; never forward them to Android.
	d := device.NewDevice(tun, b, &device.Logger{Verbosef: func(string, ...any) {}, Errorf: func(string, ...any) {}})
	if e = d.IpcSet(c.IPC(endpoint)); e != nil {
		d.Close()
		return nil, errors.New("AWG rejected configuration")
	}
	if e = d.Up(); e != nil {
		d.Close()
		return nil, e
	}
	engine := &Engine{device: d, network: n, bind: b}
	for _, s := range c.Allowed {
		engine.allowed = append(engine.allowed, netip.MustParsePrefix(s))
	}
	return engine, nil
}
func (e *Engine) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	h, p, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip, err := netip.ParseAddr(h)
	if err != nil {
		a, err := e.network.LookupContextHost(ctx, h)
		if err != nil {
			return nil, err
		}
		for _, s := range a {
			v, x := netip.ParseAddr(s)
			if x == nil && v.Is4() {
				ip = v
				break
			}
		}
	}
	if !ip.Is4() {
		return nil, errors.New("IPv4 destination required")
	}
	allowed := false
	for _, p := range e.allowed {
		allowed = allowed || p.Contains(ip)
	}
	if !allowed {
		return nil, errors.New("destination outside peer AllowedIPs")
	}
	if network == "ping4" {
		return e.network.DialContext(ctx, network, ip.String())
	}
	return e.network.DialContext(ctx, network, net.JoinHostPort(ip.String(), p))
}
func (e *Engine) Migrate(binder Binder) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.device == nil {
		return net.ErrClosed
	}
	e.bind.mu.Lock()
	e.bind.binder = binder
	e.bind.mu.Unlock()
	return e.device.BindUpdate()
}
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.device != nil {
		e.device.Close()
		e.device = nil
	}
}
