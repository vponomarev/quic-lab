package vless

import (
	"context"
	"errors"
	"net"
	"sync"

	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	xlog "github.com/xtls/xray-core/common/log"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/transport/internet"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
)

var engineInit sync.Mutex
var installRouter sync.Once
var physicalSockets = newSocketRouter()

// engine accepts only programmatically built, restricted configurations. The
// public URI/JSON importer is deliberately not exposed by this V0 boundary.
type engine struct {
	instance *core.Instance
	ctx      context.Context
	cancel   context.CancelFunc
	stop     func() bool
	stopMu   sync.Mutex
	once     sync.Once
	err      error
}

func newEngine(parent context.Context, cfg *core.Config, f SocketFactory) (*engine, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if f == nil {
		return nil, ErrUnknownEngine
	}
	engineInit.Lock()
	defer engineInit.Unlock()
	installRouter.Do(func() { internet.UseAlternativeSystemDialer(physicalSockets); xlog.RegisterHandler(privateLog{}) })
	ctx, cancel := context.WithCancel(parent)
	i, err := core.NewWithContext(ctx, cfg)
	if err != nil {
		cancel()
		return nil, errors.New("VLESS engine configuration rejected")
	}
	if err = physicalSockets.register(i, ctx, f); err != nil {
		cancel()
		i.Close()
		return nil, err
	}
	if err = i.Start(); err != nil {
		physicalSockets.unregister(i)
		cancel()
		i.Close()
		return nil, errors.New("VLESS engine start failed")
	}
	e := &engine{instance: i, ctx: ctx, cancel: cancel}
	e.stopMu.Lock()
	e.stop = context.AfterFunc(parent, func() { e.Close() })
	e.stopMu.Unlock()
	return e, nil
}
func (e *engine) Close() error {
	e.once.Do(func() {
		e.stopMu.Lock()
		if e.stop != nil {
			e.stop()
		}
		e.stopMu.Unlock()
		e.cancel()
		physicalSockets.unregister(e.instance)
		e.err = e.instance.Close()
	})
	return e.err
}
func (e *engine) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if err := e.ctx.Err(); err != nil {
		return nil, err
	}
	if network != "tcp" && network != "tcp4" {
		return nil, errors.New("VLESS network not yet supported")
	}
	dest, err := xnet.ParseDestination("tcp:" + address)
	if err != nil {
		return nil, errors.New("invalid VLESS flow destination")
	}
	call, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(e.ctx, cancel)
	c, err := core.Dial(call, e.instance, dest)
	if err != nil {
		stop()
		cancel()
		return nil, errors.New("VLESS flow rejected")
	}
	if err = call.Err(); err != nil {
		c.Close()
		stop()
		cancel()
		return nil, err
	}
	return &engineConn{Conn: c, cancel: cancel, stop: stop}, nil
}

type engineConn struct {
	net.Conn
	cancel context.CancelFunc
	stop   func() bool
	once   sync.Once
	err    error
}

func (c *engineConn) Close() error {
	c.once.Do(func() { c.stop(); c.cancel(); c.err = c.Conn.Close() })
	return c.err
}

// Client is a bounded Xray lifecycle; unsupported packet transports return errors.
type Client interface {
	DialContext(context.Context, string, string) (net.Conn, error)
	Close() error
}

func New(ctx context.Context, c Config, f SocketFactory) (Client, error) {
	cfg, err := buildConfig(c)
	if err != nil {
		return nil, err
	}
	return newEngine(ctx, cfg, f)
}

// Upstream access/error logs can contain UUIDs and destinations. Product
// diagnostics must emit their own sanitized events, never forward these messages.
type privateLog struct{}

func (privateLog) Handle(xlog.Message) {}
