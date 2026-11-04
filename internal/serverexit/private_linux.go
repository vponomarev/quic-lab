//go:build linux

package serverexit

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func privateDirectory(dir string) error {
	st, e := os.Stat(dir)
	if e != nil {
		return ErrConfig
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !st.IsDir() || st.Mode().Perm()&0077 != 0 || !ok || owner.Uid != uint32(os.Geteuid()) {
		return ErrConfig
	}
	return nil
}

func privateListener(dir, name string) (net.Listener, func(), error) {
	if privateDirectory(dir) != nil {
		return nil, nil, ErrConfig
	}
	fd, e := syscall.Open(filepath.Join(dir, "."+name+".lock"), syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return nil, nil, ErrConfig
	}
	lock := os.NewFile(uintptr(fd), "Server exit broker lock")
	st, e := lock.Stat()
	if e != nil || !st.Mode().IsRegular() || !privateFile(st, 0600) {
		lock.Close()
		return nil, nil, ErrConfig
	}
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		lock.Close()
		return nil, nil, ErrDenied
	}
	unlock := func() { syscall.Flock(fd, syscall.LOCK_UN); lock.Close() }
	path := filepath.Join(dir, name)
	if info, e := os.Lstat(path); e == nil {
		if info.Mode()&os.ModeSocket == 0 || !privateFile(info, 0600) {
			unlock()
			return nil, nil, ErrConfig
		}
		c, e := net.DialTimeout("unix", path, 100*time.Millisecond)
		if e == nil {
			c.Close()
			unlock()
			return nil, nil, ErrDenied
		}
		if !errors.Is(e, syscall.ECONNREFUSED) {
			unlock()
			return nil, nil, ErrDenied
		}
		current, e := os.Lstat(path)
		if e != nil || !os.SameFile(info, current) {
			unlock()
			return nil, nil, ErrDenied
		}
		if os.Remove(path) != nil {
			unlock()
			return nil, nil, ErrDenied
		}
	} else if !os.IsNotExist(e) {
		unlock()
		return nil, nil, ErrDenied
	}
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		unlock()
		return nil, nil, ErrDenied
	}
	l.SetUnlinkOnClose(false)
	if os.Chmod(path, 0600) != nil {
		l.Close()
		unlock()
		return nil, nil, ErrDenied
	}
	info, e := os.Lstat(path)
	if e != nil {
		l.Close()
		unlock()
		return nil, nil, ErrDenied
	}
	cleanup := func() {
		if current, e := os.Lstat(path); e == nil && os.SameFile(info, current) {
			os.Remove(path)
		}
		unlock()
	}
	return l, cleanup, nil
}
func privateFile(info os.FileInfo, mode os.FileMode) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Geteuid()) && info.Mode().Perm() == mode
}
