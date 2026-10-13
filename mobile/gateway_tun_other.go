//go:build !linux

package mobile

import "errors"

func (g *Gateway) Attach(fd int) error { return g.attachRouter(fd, nil) }
func (g *Gateway) attachRouter(fd int, router *MultiRouter) error {
	return errors.New("file descriptor TUN supported on Android/Linux only")
}
