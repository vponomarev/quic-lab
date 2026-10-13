//go:build windows

package mobile

import "gvisor.dev/gvisor/pkg/tcpip/stack"

// AttachWindowsLink takes ownership of a Windows packet endpoint; not exported in gomobile builds.
func (g *Gateway) AttachWindowsLink(link stack.LinkEndpoint) error { return g.attachLink(link, nil) }
