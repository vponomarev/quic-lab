//go:build linux

package mobile

import (
	"github.com/xjasonlyu/tun2socks/v2/core/device/fdbased"
	"golang.org/x/sys/unix"
	"strconv"
)

// Attach duplicates a borrowed Android TUN fd. The service retains its original.
func (g *Gateway) Attach(fd int) error { return g.attachRouter(fd, nil) }
func (g *Gateway) attachRouter(fd int, router *MultiRouter) error {
	dup, err := unix.Dup(fd)
	if err != nil {
		return err
	}
	unix.CloseOnExec(dup)
	dev, err := fdbased.Open(strconv.Itoa(dup), 1280, 0)
	if err != nil {
		unix.Close(dup)
		return err
	}
	return g.attachLink(dev, router)
}
