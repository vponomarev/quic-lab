//go:build linux && !android

package mobile

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDesktopLinuxAttachClosedPreservesBorrowedFD(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	router := &DesktopAppRouter{closed: true}
	if err := router.AttachLinuxFD(int(reader.Fd())); err == nil {
		t.Fatal("closed router accepted device")
	}
	var info unix.Stat_t
	if err := unix.Fstat(int(reader.Fd()), &info); err != nil {
		t.Fatal("borrowed descriptor was closed", err)
	}
}
