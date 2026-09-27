package gateway

import (
	"context"
	"crypto/tls"
	"net"
)

type captureContextKey struct{}
type captureWrap func(string, string, net.Conn) net.Conn

func (s *Server) captureContext(ctx context.Context, cs tls.ConnectionState) context.Context {
	if s.Capture == nil {
		return ctx
	}
	return context.WithValue(ctx, captureContextKey{}, captureWrap(func(network, address string, c net.Conn) net.Conn { return s.Capture(cs, network, address, c) }))
}
func captureConnection(ctx context.Context, network, address string, c net.Conn) net.Conn {
	if wrap, ok := ctx.Value(captureContextKey{}).(captureWrap); ok {
		return wrap(network, address, c)
	}
	return c
}
