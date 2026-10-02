//go:build linux

package awgserver

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

func reloadPath(dir string) string { return filepath.Join(dir, "awg-reload.sock") }
func privateDirectory(dir string) error {
	info, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 || !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("AWG control directory must be private and owned by worker")
	}
	return nil
}

// Reload acknowledges a serialized disk reconciliation, including peer removal.
func Reload(dir string) error {
	if e := privateDirectory(dir); e != nil {
		return e
	}
	c, e := net.DialTimeout("unix", reloadPath(dir), 2*time.Second)
	if e != nil {
		return errors.New("AWG worker reload unavailable")
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, e = c.Write([]byte("reload\n")); e != nil {
		return e
	}
	reply, e := bufio.NewReader(c).ReadString('\n')
	if e != nil {
		return e
	}
	if reply != "ok\n" {
		return errors.New("AWG worker rejected reload")
	}
	return nil
}
func (w *Worker) listenReload(ctx context.Context) (<-chan net.Conn, func(), error) {
	if e := privateDirectory(w.Dir); e != nil {
		return nil, nil, e
	}
	path := reloadPath(w.Dir)
	unlock, e := lockWorker(w.Dir)
	if e != nil {
		return nil, nil, e
	}
	ctx, cancel := context.WithCancel(ctx)
	failed := true
	defer func() {
		if failed {
			cancel()
			unlock()
		}
	}()
	if info, e := os.Lstat(path); e == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if info.Mode()&os.ModeSocket == 0 || !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != 0600 {
			return nil, nil, errors.New("foreign AWG reload path")
		}
		c, e := net.DialTimeout("unix", path, 200*time.Millisecond)
		if e == nil {
			c.Close()
			return nil, nil, errors.New("AWG reload listener already active")
		}
		if !errors.Is(e, syscall.ECONNREFUSED) {
			return nil, nil, errors.New("cannot prove AWG reload socket stale")
		}
		current, e := os.Lstat(path)
		if e != nil || !os.SameFile(info, current) {
			return nil, nil, errors.New("AWG reload socket changed during recovery")
		}
		if e = os.Remove(path); e != nil {
			return nil, nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, nil, e
	}
	listener, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		return nil, nil, e
	}
	listener.SetUnlinkOnClose(false)
	if e = os.Chmod(path, 0600); e != nil {
		listener.Close()
		return nil, nil, e
	}
	socketInfo, e := os.Lstat(path)
	if e != nil {
		listener.Close()
		return nil, nil, e
	}
	requests := make(chan net.Conn)
	go func() {
		for {
			c, e := listener.AcceptUnix()
			if e != nil {
				return
			}
			c.SetDeadline(time.Now().Add(2 * time.Second))
			command, e := bufio.NewReader(c).ReadString('\n')
			if e != nil || command != "reload\n" {
				c.Close()
				continue
			}
			select {
			case requests <- c:
			case <-ctx.Done():
				c.Close()
				return
			}
		}
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			listener.Close()
			if current, e := os.Lstat(path); e == nil && os.SameFile(socketInfo, current) {
				os.Remove(path)
			}
			unlock()
		})
	}
	failed = false
	return requests, stop, nil
}

// Persistent lock inode gives all worker starts exclusive ownership while
// recovering an owned, refused socket. Never unlink the lock file itself.
func lockWorker(dir string) (func(), error) {
	fd, e := syscall.Open(filepath.Join(dir, ".awg-worker.lock"), syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	file := os.NewFile(uintptr(fd), "AWG worker lock")
	info, e := file.Stat()
	if e != nil {
		file.Close()
		return nil, e
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || stat.Uid != uint32(os.Geteuid()) {
		file.Close()
		return nil, errors.New("foreign AWG worker lock")
	}
	if e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		file.Close()
		return nil, errors.New("AWG worker already owns control directory")
	}
	return func() { syscall.Flock(fd, syscall.LOCK_UN); file.Close() }, nil
}
