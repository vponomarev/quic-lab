//go:build linux

package awgserver

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const AdmissionLeaseTTL = 2 * time.Second

func admissionPath(dir string) string { return filepath.Join(dir, "awg-admission.sock") }
func RequestAdmission(dir, id string) (time.Duration, error) {
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "\r\n ") {
		return 0, errors.New("invalid admission identity")
	}
	if e := privateDirectory(dir); e != nil {
		return 0, e
	}
	c, e := net.DialTimeout("unix", admissionPath(dir), 100*time.Millisecond)
	if e != nil {
		return 0, e
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(100 * time.Millisecond))
	if _, e = c.Write([]byte(id + "\n")); e != nil {
		return 0, e
	}
	reply, e := bufio.NewReader(c).ReadString('\n')
	if e != nil {
		return 0, e
	}
	ms, e := strconv.Atoi(strings.TrimSpace(reply))
	if e != nil || ms <= 0 || ms > int(AdmissionLeaseTTL/time.Millisecond) {
		return 0, errors.New("admission denied")
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// ServeAdmission serializes bounded private IPC requests. The callback validates
// current device policy and renews the same Admission used by QUIC and HTTPS.
func ServeAdmission(ctx context.Context, dir string, renew func(string) (time.Duration, error)) (func(), error) {
	if e := privateDirectory(dir); e != nil {
		return nil, e
	}
	fd, e := syscall.Open(filepath.Join(dir, ".awg-admission.lock"), syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	lock := os.NewFile(uintptr(fd), "admission lock")
	info, e := lock.Stat()
	if e != nil {
		lock.Close()
		return nil, e
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || stat.Uid != uint32(os.Geteuid()) {
		lock.Close()
		return nil, errors.New("foreign admission lock")
	}
	if e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		lock.Close()
		return nil, errors.New("admission broker already active")
	}
	unlock := func() { syscall.Flock(fd, syscall.LOCK_UN); lock.Close() }
	path := admissionPath(dir)
	if info, e := os.Lstat(path); e == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 || !ok || stat.Uid != uint32(os.Geteuid()) {
			unlock()
			return nil, errors.New("foreign admission socket")
		}
		c, e := net.DialTimeout("unix", path, 100*time.Millisecond)
		if e == nil {
			c.Close()
			unlock()
			return nil, errors.New("admission listener already active")
		}
		if !errors.Is(e, syscall.ECONNREFUSED) {
			unlock()
			return nil, errors.New("cannot prove admission socket stale")
		}
		current, e := os.Lstat(path)
		if e != nil || !os.SameFile(info, current) {
			unlock()
			return nil, errors.New("admission socket changed during recovery")
		}
		if e = os.Remove(path); e != nil {
			unlock()
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		unlock()
		return nil, e
	}
	listener, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		unlock()
		return nil, e
	}
	listener.SetUnlinkOnClose(false)
	if e = os.Chmod(path, 0600); e != nil {
		listener.Close()
		unlock()
		return nil, e
	}
	socketInfo, e := os.Lstat(path)
	if e != nil {
		listener.Close()
		unlock()
		return nil, e
	}
	child, cancel := context.WithCancel(ctx)
	var once sync.Once
	var mu sync.Mutex
	var pending *net.UnixConn
	stop := func() {
		once.Do(func() {
			cancel()
			listener.Close()
			mu.Lock()
			if pending != nil {
				pending.Close()
			}
			mu.Unlock()
			if current, e := os.Lstat(path); e == nil && os.SameFile(socketInfo, current) {
				os.Remove(path)
			}
			unlock()
		})
	}
	go func() { <-child.Done(); stop() }()
	go func() {
		defer stop()
		for {
			c, e := listener.AcceptUnix()
			if e != nil {
				return
			}
			mu.Lock()
			pending = c
			mu.Unlock()
			c.SetDeadline(time.Now().Add(100 * time.Millisecond))
			line, e := bufio.NewReaderSize(c, 256).ReadSlice('\n')
			id := string(line)
			reply := "0\n"
			if e == nil && len(id) <= 129 && !strings.ContainsAny(strings.TrimSuffix(id, "\n"), "\r ") {
				if ttl, e := renew(strings.TrimSuffix(id, "\n")); e == nil {
					reply = strconv.FormatInt(ttl.Milliseconds(), 10) + "\n"
				}
			}
			c.Write([]byte(reply))
			c.Close()
			mu.Lock()
			pending = nil
			mu.Unlock()
		}
	}()
	return stop, nil
}
