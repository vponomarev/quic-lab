//go:build linux && !android

package mobile

import (
	"github.com/xjasonlyu/tun2socks/v2/core/device/fdbased"
	"golang.org/x/sys/unix"
	"strconv"
)

// AttachLinuxFD duplicates the client's borrowed TUN descriptor. The router
// owns only the duplicate, including failed attachment; Client owns the original.
func (r *DesktopAppRouter) AttachLinuxFD(fd int) error {
	duplicate, e := unix.Dup(fd)
	if e != nil {
		return e
	}
	unix.CloseOnExec(duplicate)
	dev, e := fdbased.Open(strconv.Itoa(duplicate), 1280, 0)
	if e != nil {
		unix.Close(duplicate)
		return e
	}
	return r.Attach(newLinuxICMPLink(dev, r.echoLinux, func(n int) { r.icmpRX.Add(int64(n)) }))
}
