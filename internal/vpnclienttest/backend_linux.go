//go:build linux

// Package vpnclienttest is a bounded Linux architecture probe, not a product backend.
package vpnclienttest

import (
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"quiclab/internal/vpnclient"
	"quiclab/mobile"
	"regexp"
)

type device interface {
	Fd() uintptr
	Close() error
}
type Backend struct {
	name, raw string
	open      func(string) (device, error)
	dev       device
	gateway   *mobile.Gateway
}

func New(name, raw string) *Backend { return &Backend{name: name, raw: raw, open: openTUN} }
func openTUN(name string) (device, error) {
	if !regexp.MustCompile(`^ql[a-zA-Z0-9_-]{1,12}$`).MatchString(name) {
		return nil, errors.New("invalid probe adapter name")
	}
	f, e := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if e != nil {
		return nil, e
	}
	req, e := unix.NewIfreq(name)
	if e == nil {
		req.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI)
		e = unix.IoctlIfreq(int(f.Fd()), unix.TUNSETIFF, req)
	}
	if e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}
func (b *Backend) Start(ctx context.Context, c vpnclient.Config) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if b.dev != nil {
		return errors.New("probe already running")
	}
	var wire struct {
		Transport string `json:"transport"`
		Endpoint  string `json:"endpoint"`
	}
	if json.Unmarshal([]byte(b.raw), &wire) != nil || len(c.Profiles.Profiles) != 1 {
		return errors.New("invalid probe configuration")
	}
	p := c.Profiles.Profiles[0]
	if p.Transport != wire.Transport || p.Endpoint != wire.Endpoint {
		return errors.New("probe profile mismatch")
	}
	d, e := b.open(b.name)
	if e != nil {
		return e
	}
	b.dev = d
	b.gateway = mobile.NewGateway(nil)
	if e = b.gateway.Start(b.raw, nil); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return b.gateway.Attach(int(d.Fd()))
}
func (b *Backend) Stop(context.Context) error {
	if b.gateway != nil {
		b.gateway.Stop()
		b.gateway = nil
	}
	if b.dev != nil {
		e := b.dev.Close()
		b.dev = nil
		return e
	}
	return nil
}
