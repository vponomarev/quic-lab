// Package vless contains the restricted Xray integration boundary.
package vless

import (
	"context"
	"errors"
	"net"
	"sync"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/transport/internet"
)

var ErrUnknownEngine = errors.New("VLESS socket has no active owner")
var ErrUnsupportedSocket = errors.New("unsupported VLESS outer socket configuration")

// SocketFactory must protect, bind, resolve and meter physical sockets before
// returning them. It must never fall back to an unprotected system dialer.
type SocketFactory interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type socketOwner struct {
	ctx     context.Context
	cancel  context.CancelFunc
	factory SocketFactory
	conns   map[*ownedConn]struct{}
	stop    func() bool
}

// socketRouter is installed once, before any Xray instance is constructed.
// Instance identity selects a factory; the active physical network is never global.
type socketRouter struct {
	mu     sync.Mutex
	owners map[*core.Instance]*socketOwner
}

func newSocketRouter() *socketRouter {
	return &socketRouter{owners: make(map[*core.Instance]*socketOwner)}
}
func (r *socketRouter) register(i *core.Instance, parent context.Context, f SocketFactory) error {
	if i == nil || f == nil {
		return ErrUnknownEngine
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.owners[i]; ok {
		return errors.New("VLESS owner already registered")
	}
	if err := parent.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	o := &socketOwner{ctx: ctx, cancel: cancel, factory: f, conns: make(map[*ownedConn]struct{})}
	r.owners[i] = o
	o.stop = context.AfterFunc(parent, func() { r.unregister(i) })
	return nil
}
func (r *socketRouter) unregister(i *core.Instance) {
	r.mu.Lock()
	o := r.owners[i]
	if o == nil {
		r.mu.Unlock()
		return
	}
	delete(r.owners, i)
	o.cancel()
	o.stop()
	conns := make([]*ownedConn, 0, len(o.conns))
	for c := range o.conns {
		conns = append(conns, c)
	}
	r.mu.Unlock()
	for _, c := range conns {
		c.Close()
	}
}
func (r *socketRouter) Dial(ctx context.Context, source xnet.Address, dest xnet.Destination, opts *internet.SocketConfig) (net.Conn, error) {
	// Advanced Xray DNS/dialerProxy strategies use package globals and are not
	// permitted at this boundary. UDP underlay is a separate, unimplemented contract.
	if dest.Network != xnet.Network_TCP || opts != nil || source != nil {
		return nil, ErrUnsupportedSocket
	}
	i := core.FromContext(ctx)
	r.mu.Lock()
	o := r.owners[i]
	r.mu.Unlock()
	if o == nil {
		return nil, ErrUnknownEngine
	}
	dialCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(o.ctx, cancel)
	defer cancel()
	defer stop()
	if err := o.ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := o.factory.DialContext(dialCtx, "tcp4", dest.NetAddr())
	if err != nil {
		if conn != nil {
			conn.Close()
		}
		return nil, err
	}
	if conn == nil {
		return nil, errors.New("VLESS socket factory returned no connection")
	}
	r.mu.Lock()
	if r.owners[i] != o || o.ctx.Err() != nil || dialCtx.Err() != nil {
		r.mu.Unlock()
		conn.Close()
		return nil, context.Canceled
	}
	c := &ownedConn{Conn: conn, router: r, owner: o}
	c.stopMu.Lock()
	o.conns[c] = struct{}{}
	r.mu.Unlock()
	c.stop = context.AfterFunc(ctx, func() { c.Close() })
	c.stopMu.Unlock()
	return c, nil
}
func (*socketRouter) DestIpAddress() xnet.IP { return nil }

type ownedConn struct {
	net.Conn
	router *socketRouter
	owner  *socketOwner
	stopMu sync.Mutex
	stop   func() bool
	once   sync.Once
	err    error
}

func (c *ownedConn) Close() error {
	c.once.Do(func() {
		c.stopMu.Lock()
		if c.stop != nil {
			c.stop()
		}
		c.stopMu.Unlock()
		c.router.mu.Lock()
		delete(c.owner.conns, c)
		c.router.mu.Unlock()
		c.err = c.Conn.Close()
	})
	return c.err
}
