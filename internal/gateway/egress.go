package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
)

// Egress belongs to one authenticated request. Never mutate the shared Server
// when selecting a device's exit: concurrent clients may use different providers.
type Egress struct {
	DialContext func(context.Context, string, string) (net.Conn, error)
	Resolver    *net.Resolver
	Probe       func(context.Context) error
	ProbeICMP   func(context.Context, []byte) ([]byte, error)
}
type egressIdentityKey struct{}
type egressKey struct{}

func (s *Server) selectedEgress(ctx context.Context) (context.Context, error) {
	if s.Egress == nil {
		return ctx, nil
	}
	cs, ok := ctx.Value(egressIdentityKey{}).(tls.ConnectionState)
	if !ok {
		return ctx, errors.New("missing exit identity")
	}
	e, err := s.Egress(cs)
	if err != nil {
		return ctx, err
	}
	if e == nil {
		return ctx, nil
	} // explicit legacy selection only
	if e.DialContext == nil || e.Resolver == nil {
		return ctx, errors.New("exit has no dialer or resolver")
	}
	return context.WithValue(ctx, egressKey{}, e), nil
}
func (s *Server) egress(ctx context.Context) *Egress {
	if e, ok := ctx.Value(egressKey{}).(*Egress); ok {
		return e
	}
	return &Egress{DialContext: s.DialContext, Resolver: s.Resolver, Probe: s.Probe, ProbeICMP: s.ProbeICMP}
}
func (s *Server) dial(ctx context.Context, network, address string) (net.Conn, error) {
	dial := s.egress(ctx).DialContext
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	return dial(ctx, network, address)
}
