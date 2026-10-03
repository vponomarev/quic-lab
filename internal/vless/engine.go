package vless

import (
	"context"
	"errors"
	"net"
	"sync"

	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	"github.com/xtls/xray-core/common"
	xlog "github.com/xtls/xray-core/common/log"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/routing"
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

// CreateObject supplies the instance context through Xray's public extension
// mechanism. No private context keys are reproduced by the adapter.
type flowContextConfig struct{}

func init() {
	common.Must(common.RegisterConfig((*flowContextConfig)(nil), func(ctx context.Context, _ interface{}) (interface{}, error) { return ctx, nil }))
}
func (e *engine) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if err := e.ctx.Err(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	packet := network == "udp" || network == "udp4"
	if network != "tcp" && network != "tcp4" && !packet {
		return nil, errors.New("unsupported VLESS network")
	}
	protocol := "tcp:"
	if packet {
		protocol = "udp:"
	}
	dest, err := xnet.ParseDestination(protocol + address)
	if err != nil || !dest.IsValid() {
		return nil, errors.New("invalid VLESS flow destination")
	}
	if dest.Address.Family().IsIPv6() {
		return nil, errors.New("IPv6 VLESS destination unsupported")
	}
	obj, err := core.CreateObject(e.instance, &flowContextConfig{})
	if err != nil {
		return nil, errors.New("VLESS flow context unavailable")
	}
	call, cancel := context.WithCancel(obj.(context.Context))
	// Dial context controls establishment, not the returned connection's lifetime.
	c, link := newFlowWithClose(call, packet, cancel)
	dispatcher, ok := e.instance.GetFeature(routing.DispatcherType()).(routing.Dispatcher)
	if !ok {
		c.Close()
		return nil, errors.New("VLESS dispatcher unavailable")
	}
	go func() {
		if dispatchErr := dispatcher.DispatchLink(call, dest, link); dispatchErr != nil {
			c.Close()
			return
		}
		c.up.Interrupt()
		c.down.Close()
	}()
	if err = ctx.Err(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
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
