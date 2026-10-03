package vless

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/features/routing"
	account "github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/transport"
)

var ErrDeviceRevoked = errors.New("VLESS device is unavailable")

type deviceFlow struct {
	conn   net.Conn
	cancel context.CancelFunc
	once   sync.Once
}

func (f *deviceFlow) close() { f.once.Do(func() { f.cancel(); f.conn.Close() }) }

// deviceDispatcher is a server-only gate after VLESS authentication. It closes
// the original TLS/REALITY connection; wrapping that connection would break Vision.
type deviceDispatcher struct {
	base      routing.Dispatcher
	admission func(context.Context, string) (time.Duration, error)
	mu        sync.Mutex
	closed    bool
	revoked   map[string]bool
	flows     map[string]map[*deviceFlow]struct{}
	once      sync.Once
	err       error
}

func newDeviceDispatcher(base routing.Dispatcher) *deviceDispatcher {
	return &deviceDispatcher{base: base, revoked: map[string]bool{}, flows: map[string]map[*deviceFlow]struct{}{}}
}
func (d *deviceDispatcher) Type() interface{} { return routing.DispatcherType() }
func (d *deviceDispatcher) Start() error      { return d.base.Start() }
func (d *deviceDispatcher) Dispatch(context.Context, xnet.Destination) (*transport.Link, error) {
	return nil, ErrDeviceRevoked
}
func (d *deviceDispatcher) DispatchLink(ctx context.Context, dest xnet.Destination, link *transport.Link) error {
	return d.dispatchVia(ctx, dest, link, d.base)
}
func (d *deviceDispatcher) dispatchVia(ctx context.Context, dest xnet.Destination, link *transport.Link, next routing.Dispatcher) error {
	in := session.InboundFromContext(ctx)
	if in == nil || in.Name != "vless" || in.User == nil || in.Conn == nil {
		return ErrDeviceRevoked
	}
	a, ok := in.User.Account.(*account.MemoryAccount)
	if !ok || a.ID == nil {
		in.Conn.Close()
		return ErrDeviceRevoked
	}
	uid := a.ID.UUID()
	id := uid.String()
	call, cancel := context.WithCancel(ctx)
	f := &deviceFlow{conn: in.Conn, cancel: cancel}
	if d.admission != nil {
		if err := d.startLease(call, id, f); err != nil {
			f.close()
			return ErrDeviceRevoked
		}
	}
	d.mu.Lock()
	if d.closed || d.revoked[id] || ctx.Err() != nil {
		d.mu.Unlock()
		f.close()
		return ErrDeviceRevoked
	}
	if d.flows[id] == nil {
		d.flows[id] = map[*deviceFlow]struct{}{}
	}
	d.flows[id][f] = struct{}{}
	d.mu.Unlock()
	defer func() {
		f.close()
		d.mu.Lock()
		delete(d.flows[id], f)
		if len(d.flows[id]) == 0 {
			delete(d.flows, id)
		}
		d.mu.Unlock()
	}()
	return next.DispatchLink(call, dest, link)
}
func (d *deviceDispatcher) Revoke(id string) {
	if !validUUID(id) {
		return
	}
	id = strings.ToLower(id)
	d.mu.Lock()
	d.revoked[id] = true
	list := make([]*deviceFlow, 0, len(d.flows[id]))
	for f := range d.flows[id] {
		list = append(list, f)
	}
	d.mu.Unlock()
	for _, f := range list {
		f.close()
	}
}
func (d *deviceDispatcher) Close() error {
	d.once.Do(func() {
		d.mu.Lock()
		d.closed = true
		var list []*deviceFlow
		for _, set := range d.flows {
			for f := range set {
				list = append(list, f)
			}
		}
		d.mu.Unlock()
		for _, f := range list {
			f.close()
		}
		if d.base != nil {
			d.err = d.base.Close()
		}
	})
	return d.err
}

// AuthenticatedPeer returns diagnostic source metadata inside an admission
// callback. It is not an authorization identity; VLESS authentication and the
// device UUID remain authoritative. The original TLS/REALITY conn is untouched.
func AuthenticatedPeer(ctx context.Context) string {
	in := session.InboundFromContext(ctx)
	if in == nil || in.Name != "vless" || in.User == nil || in.Conn == nil || in.Conn.RemoteAddr() == nil {
		return ""
	}
	return in.Conn.RemoteAddr().String()
}
