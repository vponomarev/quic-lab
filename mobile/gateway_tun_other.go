//go:build !linux

package mobile

import "errors"

func (g *Gateway) Attach(fd int) error { return errors.New("TUN supported on Android/Linux only") }
