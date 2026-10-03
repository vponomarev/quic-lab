package main

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"

	"github.com/quic-go/quic-go"
	"quiclab/internal/gateway"
	"quiclab/internal/protocol"
)

// A stalled consumer has a bounded backlog without blocking the other protocol.
const sharedQUICQueueSize = 128

type sharedQUICListener struct {
	Echo     *sharedQUICAcceptQueue
	VPN      *sharedQUICAcceptQueue
	listener *quic.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	active   map[*quic.Conn]func() bool
}

type sharedQUICAcceptQueue struct {
	owner       *sharedQUICListener
	connections chan *quic.Conn
}

// newSharedQUICListener owns the UDP socket and dispatches completed handshakes.
// The caller supplies the gateway's stream and datagram settings for shared use.
func newSharedQUICListener(ctx context.Context, address string, echoTLS, vpnTLS *tls.Config, config *quic.Config) (*sharedQUICListener, error) {
	if echoTLS == nil || vpnTLS == nil {
		return nil, errors.New("shared QUIC requires echo and VPN TLS configurations")
	}
	e, v := echoTLS.Clone(), vpnTLS.Clone()
	combined := e.Clone()
	combined.MinVersion = tls.VersionTLS13
	combined.NextProtos = []string{gateway.ALPN, gateway.BondALPN, protocol.ALPN}
	combined.SessionTicketsDisabled = true
	combined.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		// An offer containing any VPN ALPN always uses VPN-only negotiation and
		// authentication, even when echo precedes it in the client's preference list.
		vpn, echo := false, false
		for _, alpn := range hello.SupportedProtos {
			switch alpn {
			case gateway.ALPN, gateway.BondALPN:
				vpn = true
			case protocol.ALPN:
				echo = true
			}
		}
		if !vpn && !echo {
			return nil, errors.New("unsupported QUIC ALPN")
		}
		selected := e
		if vpn {
			selected = v
		}
		if selected.GetConfigForClient != nil {
			replacement, err := selected.GetConfigForClient(hello)
			if err != nil {
				return nil, err
			}
			if replacement != nil {
				selected = replacement
			}
		}
		result := selected.Clone()
		result.GetConfigForClient = nil
		result.MinVersion = tls.VersionTLS13
		// Tickets must never carry an unauthenticated echo identity into a VPN
		// handshake. Disabling them also ensures managed admission runs each time.
		result.SessionTicketsDisabled = true
		if vpn {
			result.NextProtos = []string{gateway.ALPN, gateway.BondALPN}
			result.ClientAuth = tls.RequireAndVerifyClientCert
		} else {
			result.NextProtos = []string{protocol.ALPN}
			result.ClientAuth = tls.NoClientCert
		}
		return result, nil
	}
	listener, err := quic.ListenAddr(address, combined, config)
	if err != nil {
		return nil, err
	}
	dispatchCtx, cancel := context.WithCancel(ctx)
	s := &sharedQUICListener{listener: listener, ctx: dispatchCtx, cancel: cancel, done: make(chan struct{}), active: make(map[*quic.Conn]func() bool)}
	s.Echo = &sharedQUICAcceptQueue{owner: s, connections: make(chan *quic.Conn, sharedQUICQueueSize)}
	s.VPN = &sharedQUICAcceptQueue{owner: s, connections: make(chan *quic.Conn, sharedQUICQueueSize)}
	go s.dispatch()
	return s, nil
}

func (s *sharedQUICListener) Addr() net.Addr { return s.listener.Addr() }

func (q *sharedQUICAcceptQueue) Accept(ctx context.Context) (*quic.Conn, error) {
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if q.owner.ctx.Err() != nil {
			return nil, quic.ErrServerClosed
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-q.owner.ctx.Done():
			return nil, quic.ErrServerClosed
		case c := <-q.connections:
			if q.owner.ctx.Err() != nil {
				return nil, quic.ErrServerClosed
			}
			// Disconnected clients must not occupy the consumer's accept capacity.
			if c.Context().Err() == nil {
				return c, nil
			}
		}
	}
}

func (s *sharedQUICListener) dispatch() {
	defer close(s.done)
	defer s.stop()
	for {
		c, err := s.listener.Accept(s.ctx)
		if err != nil {
			return
		}
		state := c.ConnectionState().TLS
		var queue *sharedQUICAcceptQueue
		switch state.NegotiatedProtocol {
		case protocol.ALPN:
			queue = s.Echo
		case gateway.ALPN, gateway.BondALPN:
			if len(state.VerifiedChains) != 0 {
				queue = s.VPN
			}
		}
		if queue == nil {
			c.CloseWithError(1, "QUIC authentication required")
			continue
		}
		s.mu.Lock()
		if s.ctx.Err() != nil {
			s.mu.Unlock()
			c.CloseWithError(0, "server stopping")
			return
		}
		s.active[c] = context.AfterFunc(c.Context(), func() {
			s.mu.Lock()
			delete(s.active, c)
			s.mu.Unlock()
		})
		select {
		case queue.connections <- c:
			s.mu.Unlock()
		default:
			stop := s.active[c]
			delete(s.active, c)
			s.mu.Unlock()
			stop()
			c.CloseWithError(1, "QUIC accept queue full")
		}
	}
}

func (s *sharedQUICListener) stop() {
	s.once.Do(func() {
		s.cancel()
		s.listener.Close()
		s.mu.Lock()
		connections := make([]*quic.Conn, 0, len(s.active))
		for c, stop := range s.active {
			stop()
			connections = append(connections, c)
		}
		clear(s.active)
		s.mu.Unlock()
		for _, c := range connections {
			c.CloseWithError(0, "server stopping")
		}
		// Release references held by the bounded queues as well as active handlers.
		for _, q := range []*sharedQUICAcceptQueue{s.Echo, s.VPN} {
			for {
				select {
				case <-q.connections:
				default:
					goto drained
				}
			}
		drained:
		}
	})
}

func (s *sharedQUICListener) Close() error {
	s.stop()
	<-s.done
	return nil
}
