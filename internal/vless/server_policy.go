package vless

import (
	"context"
	"errors"
	"github.com/xtls/xray-core/app/dispatcher"
	"github.com/xtls/xray-core/common"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/features/policy"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/proxy"
	inbound "github.com/xtls/xray-core/proxy/vless/inbound"
	"github.com/xtls/xray-core/transport"
	"github.com/xtls/xray-core/transport/internet/stat"
	"google.golang.org/protobuf/proto"
)

func init() {
	common.Must(common.RegisterConfig((*ServerDispatcherConfig)(nil), func(ctx context.Context, _ interface{}) (interface{}, error) {
		sc, ok := ctx.Value(serverContextKey{}).(*serverContext)
		if !ok {
			return nil, errors.New("server context required")
		}
		base, e := common.CreateObject(ctx, &dispatcher.Config{})
		if e != nil {
			return nil, e
		}
		return &serverPolicyDispatcher{Dispatcher: base.(routing.Dispatcher), server: sc}, nil
	}))
	common.Must(common.RegisterConfig((*ServerInboundConfig)(nil), func(ctx context.Context, raw interface{}) (interface{}, error) {
		sc, ok := ctx.Value(serverContextKey{}).(*serverContext)
		if !ok {
			return nil, errors.New("server context required")
		}
		cfg := &inbound.Config{}
		if e := proto.Unmarshal(raw.(*ServerInboundConfig).VlessConfig, cfg); e != nil {
			return nil, errors.New("invalid server inbound")
		}
		p, e := common.CreateObject(ctx, cfg)
		if e != nil {
			return nil, e
		}
		return &serverInbound{Inbound: p.(proxy.Inbound), server: sc}, nil
	}))
}

// Wrap the dispatcher passed to Process, not the TLS/REALITY connection.
// This gate sees authentication BEFORE upstream mux divides it into child flows.
type serverInbound struct {
	proxy.Inbound
	server *serverContext
}

func (p *serverInbound) Process(ctx context.Context, n xnet.Network, c stat.Connection, d routing.Dispatcher) error {
	return p.Inbound.Process(ctx, n, c, &outerServerDispatch{Dispatcher: d, server: p.server})
}
func (p *serverInbound) Close() error { return common.Close(p.Inbound) }

type outerServerDispatch struct {
	routing.Dispatcher
	server *serverContext
}

func (d *outerServerDispatch) DispatchLink(ctx context.Context, dest xnet.Destination, link *transport.Link) error {
	if d.server.mode == "demux-only" && !d.server.permits(dest) {
		return ErrDeviceRevoked
	}
	if dest.Address == xnet.DomainAddress("v1.mux.cool") {
		reader := newServerXUDPReader(link.Reader, d.server)
		defer reader.Close()
		link = &transport.Link{Reader: reader, Writer: link.Writer}
	}
	return d.server.gate.dispatchVia(serverDispatchContext(ctx), dest, link, d.Dispatcher)
}
func (s *serverContext) permits(dest xnet.Destination) bool {
	if !dest.IsValid() || dest.Port == 0 {
		return false
	}
	if dest.Address.Family().IsIPv6() {
		return false
	}
	if s.mode == "demux-only" {
		return dest.Network == xnet.Network_TCP && !dest.Address.Family().IsDomain() && dest.NetAddr() == s.target
	}
	return dest.Network == xnet.Network_TCP || dest.Network == xnet.Network_UDP
}

type serverPolicyDispatcher struct {
	routing.Dispatcher
	server *serverContext
}

func (d *serverPolicyDispatcher) allowed(ctx context.Context, dest xnet.Destination) bool {
	in := session.InboundFromContext(ctx)
	return ctx.Err() == nil && ctx.Value(admittedContextKey{}) == true && in != nil && in.Name == "vless" && in.User != nil && d.server.permits(dest)
}
func (d *serverPolicyDispatcher) Dispatch(ctx context.Context, dest xnet.Destination) (*transport.Link, error) {
	// Only XUDP child dispatch is supported; arbitrary native TCP mux is not.
	if dest.Network != xnet.Network_UDP || !d.allowed(ctx, dest) {
		return nil, ErrDeviceRevoked
	}
	return d.Dispatcher.Dispatch(ctx, dest)
}
func (d *serverPolicyDispatcher) DispatchLink(ctx context.Context, dest xnet.Destination, link *transport.Link) error {
	if !d.allowed(ctx, dest) {
		return ErrDeviceRevoked
	}
	return d.Dispatcher.DispatchLink(ctx, dest, link)
}

func serverDispatchContext(ctx context.Context) context.Context {
	return policy.ContextWithBufferPolicy(context.WithValue(ctx, admittedContextKey{}, true), policy.Buffer{PerConnection: 64 * 1024})
}
