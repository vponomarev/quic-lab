//go:build windows

package windowsengine

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/xjasonlyu/tun2socks/v2/core/device/iobased"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
	"io"
	"quiclab/internal/vpnclient"
	"quiclab/mobile"
	"sync"
	"time"

	"unsafe"
)

type WindowsPlatform struct{}
type tunLink struct {
	adapter  *wintun.Adapter
	session  wintun.Session
	guid     windows.GUID
	mu       sync.RWMutex
	closed   bool
	once     sync.Once
	endpoint *iobased.Endpoint
}

func (WindowsPlatform) Open(ctx context.Context, s Settings) (Link, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, err := windows.GenerateGUID()
	if err != nil {
		return nil, err
	}
	a, err := wintun.CreateAdapter("QUIC Lab MVP "+id.String()[1:9], "QUIC Lab", &id)
	if err != nil {
		return nil, err
	}
	session, err := a.StartSession(0x400000)
	if err != nil {
		a.Close()
		return nil, err
	}
	l := &tunLink{adapter: a, session: session, guid: id}
	l.endpoint, err = iobased.New(l, 1280, 0)
	if err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}
func (l *tunLink) Read(b []byte) (int, error) {
	for {
		l.mu.RLock()
		if l.closed {
			l.mu.RUnlock()
			return 0, io.EOF
		}
		p, e := l.session.ReceivePacket()
		if e == nil {
			n := copy(b, p)
			l.session.ReleaseReceivePacket(p)
			l.mu.RUnlock()
			return n, nil
		}
		if e != windows.ERROR_NO_MORE_ITEMS {
			l.mu.RUnlock()
			return 0, e
		}
		_, e = windows.WaitForSingleObject(l.session.ReadWaitEvent(), 50)
		l.mu.RUnlock()
		if e != nil {
			return 0, e
		}
	}
}
func (l *tunLink) Write(b []byte) (int, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return 0, io.ErrClosedPipe
	}
	if len(b) == 0 || len(b) > 65535 {
		return 0, errors.New("invalid packet")
	}
	p, e := l.session.AllocateSendPacket(len(b))
	if e != nil {
		return 0, e
	}
	copy(p, b)
	l.session.SendPacket(p)
	return len(b), nil
}
func (l *tunLink) Close() error {
	l.once.Do(func() {
		l.mu.Lock()
		l.closed = true
		l.session.End()
		l.mu.Unlock()
		if l.endpoint != nil {
			l.endpoint.Close()
		}
		l.adapter.Close()
	})
	return nil
}

// Link closes the native reader before the stack waits for its goroutines.
type ownedEndpoint struct {
	*iobased.Endpoint
	link *tunLink
}

func (e *ownedEndpoint) Close() { e.link.Close() }

type NativeGateway struct {
	meter vpnclient.TrafficMeter
	g     *mobile.Gateway
	Sink  mobile.EventSink
}

func (g *NativeGateway) Start(ctx context.Context, s Settings) error {
	g.meter.Reset(time.Now().UTC().Format(time.RFC3339Nano))
	g.g = mobile.NewGateway(g)
	return g.g.Start(s.GatewayJSON(), interfaceBinder(s.InterfaceIndex))
}
func (g *NativeGateway) Attach(l Link) error {
	t, ok := l.(*tunLink)
	if !ok {
		return errors.New("invalid Windows link")
	}
	return g.g.AttachWindowsLink(&ownedEndpoint{t.endpoint, t})
}
func (g *NativeGateway) Stop() {
	if g.g != nil {
		g.g.Stop()
		g.g = nil
	}
}

type interfaceBinder uint32

func (i interfaceBinder) Bind(fd int64) error {
	if i == 0 {
		return errors.New("physical interface required")
	}
	var v [4]byte
	binary.BigEndian.PutUint32(v[:], uint32(i))
	return windows.Setsockopt(windows.Handle(fd), windows.IPPROTO_IP, 31, (*byte)(unsafe.Pointer(&v[0])), 4)
}

func (g *NativeGateway) OnEvent(raw string) {
	g.meter.Observe(raw, time.Now().UTC())
	if g.Sink != nil {
		g.Sink.OnEvent(raw)
	}
}
func (g *NativeGateway) TrafficSnapshot() vpnclient.Traffic { return g.meter.Snapshot() }
