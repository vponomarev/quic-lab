//go:build linux && !android

package main

import (
	"errors"
	"io"
	"os"
	"quiclab/internal/linuxclient"
	"syscall"
)

// Reopen on every reload so atomic profile-file replacement is supported.
func readConfig(path string) (linuxclient.Config, error) {
	// A replaced FIFO must not block the process before its type is checked.
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return linuxclient.Config{}, errors.New("cannot read configuration")
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return linuxclient.Config{}, errors.New("configuration must be a regular private file (0600)")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return linuxclient.Config{}, errors.New("cannot read configuration")
	}
	return linuxclient.Decode(raw)
}
