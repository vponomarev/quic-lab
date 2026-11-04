package main

import (
	"context"
	"crypto/tls"
	"net"
	"net/netip"
	"path/filepath"
	"quiclab/internal/admin"
	"quiclab/internal/awgserver"
	"quiclab/internal/gateway"
	"quiclab/internal/icmprelay"
	"quiclab/internal/serverexit"
	"quiclab/internal/transit"
	"quiclab/internal/vlessserver"
	"sync/atomic"
	"time"
)

func startServerExits(ctx context.Context, cfg admin.Config, store *admin.Store) (*serverexit.Runtime, func(), error) {
	if err := store.ConfigureServerExits(cfg.ServerExits); err != nil {
		return nil, nil, err
	}
	if cfg.ManagementOnly {
		return nil, func() {}, nil
	}
	catalog, err := serverexit.NewCatalog(cfg.ServerExits)
	if err != nil {
		return nil, nil, err
	}
	// Own the broker lock before starting any AWG peer, including during restart.
	var ready atomic.Pointer[serverexit.Runtime]
	legacy := &legacyServerExit{transit: cfg.Transit}
	var nativeGateway netip.Addr
	if cfg.AWG != nil {
		p, _ := netip.ParsePrefix(cfg.AWG.Address)
		nativeGateway = p.Addr()
	}
	broker := serverexit.Broker{Packets: serverexit.ForwardPackets, Acquire: func(protocol, identity string, close func()) (serverexit.Lease, error) {
		runtime := ready.Load()
		if runtime == nil {
			return serverexit.Lease{}, serverexit.ErrDenied
		}
		deviceID, id, source, release, err := store.AcquireServerExit(protocol, identity, close)
		if err != nil {
			return serverexit.Lease{}, err
		}
		var provider serverexit.Provider = legacy
		if id != "" {
			provider, err = runtime.Provider(id)
			if err != nil {
				release()
				return serverexit.Lease{}, err
			}
		}
		return serverexit.Lease{Provider: &capturedExit{Provider: provider, store: store, device: deviceID}, Source: source, Gateway: nativeGateway, Release: release}, nil
	}}
	stop, err := broker.Serve(ctx, cfg.DataDir)
	if err != nil {
		return nil, nil, err
	}
	var legacyDirs []string
	if cfg.Transit != nil && len(cfg.ServerExits) > 0 {
		legacyDirs = append(legacyDirs, filepath.Dir(cfg.Transit.ProbeSocket))
	}
	runtime, err := serverexit.StartOwned(ctx, catalog, cfg.DataDir, legacyDirs...)
	if err != nil {
		stop()
		return nil, nil, err
	}
	store.SetServerExitApply(func(ids []string) error {
		awg, vless := store.ServerExitWorkers(ids)
		if awg {
			if err := awgserver.ReloadExits(cfg.DataDir); err != nil {
				return err
			}
		}
		if vless {
			check, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if err := (&vlessserver.ControlClient{Dir: filepath.Join(cfg.DataDir, "vless")}).CheckExits(check); err != nil {
				return err
			}
		}
		return nil
	})
	ready.Store(runtime)
	return runtime, func() { ready.Store(nil); stop(); runtime.Close() }, nil
}

func selectedGatewayExit(store *admin.Store, runtime *serverexit.Runtime) func(tls.ConnectionState) (*gateway.Egress, error) {
	return func(cs tls.ConnectionState) (*gateway.Egress, error) {
		d, err := store.DeviceForTLS(cs)
		if err != nil {
			return nil, err
		}
		id, err := store.ActiveServerExitForDevice(d.ID)
		if err != nil {
			return nil, err
		}
		if id == "" {
			return nil, nil
		}
		p, err := runtime.Provider(id)
		if err != nil {
			return nil, err
		}
		return &gateway.Egress{DialContext: p.DialContext, Resolver: p.Resolver(), Probe: p.Probe, ProbeICMP: p.ProbeICMP}, nil
	}
}

type legacyServerExit struct{ transit *transit.Config }

type capturedExit struct {
	serverexit.Provider
	store  *admin.Store
	device string
}

func (p *capturedExit) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	c, err := p.Provider.DialContext(ctx, network, address)
	if err != nil {
		return c, err
	}
	return p.store.WrapDeviceConnection(p.device, network, address, c), nil
}

func (p *legacyServerExit) Close() {}
func (p *legacyServerExit) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if p.transit != nil {
		return p.transit.DialContext(ctx, network, address)
	}
	return (&net.Dialer{}).DialContext(ctx, network, address)
}
func (p *legacyServerExit) Resolver() *net.Resolver {
	if p.transit != nil {
		return p.transit.Resolver()
	}
	return net.DefaultResolver
}
func (p *legacyServerExit) Probe(ctx context.Context) error {
	if p.transit != nil {
		return p.transit.Probe(ctx)
	}
	return serverexit.ErrDenied
}
func (p *legacyServerExit) ProbeICMP(ctx context.Context, packet []byte) ([]byte, error) {
	if p.transit != nil {
		return p.transit.ProbeICMP(ctx, packet)
	}
	return icmprelay.Probe(ctx, "", packet)
}
