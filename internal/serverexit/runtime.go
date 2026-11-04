package serverexit

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"quiclab/internal/awg"
	"quiclab/internal/icmprelay"
	"sync"
	"time"
)

// Provider is independent of ingress and private protocol configuration.
// Future providers must implement the same no-fallback dial/probe contract.
type Provider interface {
	DialContext(context.Context, string, string) (net.Conn, error)
	Resolver() *net.Resolver
	Probe(context.Context) error
	ProbeICMP(context.Context, []byte) ([]byte, error)
	Close()
}
type Runtime struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

func Start(ctx context.Context, c *Catalog) (*Runtime, error) {
	return startRuntime(c, func(d Definition) (Provider, error) { return startAWG(ctx, d) })
}
func startRuntime(c *Catalog, start func(Definition) (Provider, error)) (*Runtime, error) {
	r := &Runtime{providers: map[string]Provider{}}
	if c == nil {
		return r, nil
	}
	for _, id := range c.order {
		d := c.definitions[id]
		if d.Disabled {
			continue
		}
		p, err := start(d)
		if err != nil || p == nil {
			if p != nil {
				p.Close()
			}
			r.Close()
			return nil, errors.New("server exit startup failed")
		}
		r.providers[id] = p
	}
	return r, nil
}
func (r *Runtime) Provider(id string) (Provider, error) {
	if r == nil {
		return nil, ErrDenied
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	p := r.providers[id]
	if p == nil {
		return nil, ErrDenied
	}
	return p, nil
}
func (r *Runtime) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	p := r.providers
	r.providers = nil
	r.mu.Unlock()
	for _, v := range p {
		v.Close()
	}
}

type awgProvider struct {
	engine              *awg.Engine
	dns, source, target string
}

func startAWG(ctx context.Context, d Definition) (Provider, error) {
	c, err := awg.Parse(d.AWGConfig)
	if err != nil {
		return nil, ErrConfig
	}
	host, port, err := net.SplitHostPort(c.Endpoint)
	if err != nil {
		return nil, ErrConfig
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		lookup, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		ips, e := net.DefaultResolver.LookupNetIP(lookup, "ip4", host)
		if e != nil || len(ips) == 0 {
			return nil, errors.New("server exit endpoint unavailable")
		}
		ip = ips[0].Unmap()
	}
	if !ip.Is4() {
		return nil, ErrConfig
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	e, err := awg.Start(c, net.JoinHostPort(ip.String(), port), nil)
	if err != nil {
		return nil, errors.New("server exit unavailable")
	}
	return &awgProvider{engine: e, dns: c.DNS, source: c.Address, target: ip.String()}, nil
}
func (p *awgProvider) Close() { p.engine.Close() }
func (p *awgProvider) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "udp", "udp4":
	case "ip4:icmp":
		network, address = "ping4", net.JoinHostPort(address, "0")
	default:
		return nil, ErrDenied
	}
	return p.engine.DialContext(ctx, network, address)
}
func (p *awgProvider) Resolver() *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return p.DialContext(ctx, network, net.JoinHostPort(p.dns, "53"))
	}}
}
func (p *awgProvider) ProbeICMP(ctx context.Context, packet []byte) ([]byte, error) {
	return p.engine.EchoPacket(ctx, packet)
}
func (p *awgProvider) Probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	packet := make([]byte, 44)
	packet[0], packet[8], packet[9], packet[20] = 0x45, 64, 1, 8
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	src, dst := netip.MustParseAddr(p.source).As4(), netip.MustParseAddr(p.target).As4()
	copy(packet[12:16], src[:])
	copy(packet[16:20], dst[:])
	if _, err := rand.Read(packet[24:]); err != nil {
		return err
	}
	binary.BigEndian.PutUint16(packet[10:12], icmprelay.Checksum(packet[:20]))
	binary.BigEndian.PutUint16(packet[22:24], icmprelay.Checksum(packet[20:]))
	reply, err := p.ProbeICMP(ctx, packet)
	if err != nil {
		return err
	}
	if !icmprelay.ValidateResponse(packet, reply) {
		return errors.New("server exit probe failed")
	}
	return nil
}
