//go:build linux

package vlessserver

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func privateDirectory(dir string) error {
	st, e := os.Lstat(dir)
	if e != nil {
		return errConfig
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !st.IsDir() || st.Mode().Perm()&0077 != 0 || !ok || owner.Uid != uint32(os.Geteuid()) {
		return errConfig
	}
	return nil
}

func privateListener(dir, name string) (net.Listener, func(), error) {
	if privateDirectory(dir) != nil {
		return nil, nil, errConfig
	}
	fd, e := syscall.Open(filepath.Join(dir, "."+name+".lock"), syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return nil, nil, errConfig
	}
	lock := os.NewFile(uintptr(fd), "VLESS control lock")
	st, e := lock.Stat()
	if e != nil || !st.Mode().IsRegular() || !privateFile(st, 0600) {
		lock.Close()
		return nil, nil, errConfig
	}
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		lock.Close()
		return nil, nil, errApply
	}
	unlock := func() { syscall.Flock(fd, syscall.LOCK_UN); lock.Close() }
	path := filepath.Join(dir, name)
	if info, e := os.Lstat(path); e == nil {
		if info.Mode()&os.ModeSocket == 0 || !privateFile(info, 0600) {
			unlock()
			return nil, nil, errConfig
		}
		c, e := net.DialTimeout("unix", path, 100*time.Millisecond)
		if e == nil {
			c.Close()
			unlock()
			return nil, nil, errApply
		}
		if !errors.Is(e, syscall.ECONNREFUSED) {
			unlock()
			return nil, nil, errApply
		}
		current, e := os.Lstat(path)
		if e != nil || !os.SameFile(info, current) {
			unlock()
			return nil, nil, errApply
		}
		if os.Remove(path) != nil {
			unlock()
			return nil, nil, errApply
		}
	} else if !os.IsNotExist(e) {
		unlock()
		return nil, nil, errApply
	}
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		unlock()
		return nil, nil, errApply
	}
	l.SetUnlinkOnClose(false)
	if os.Chmod(path, 0600) != nil {
		l.Close()
		unlock()
		return nil, nil, errApply
	}
	info, e := os.Lstat(path)
	if e != nil {
		l.Close()
		unlock()
		return nil, nil, errApply
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
