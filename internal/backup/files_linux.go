//go:build linux

package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const publicationFile = ".backup-publication.lock"

func openLock(path string, create bool) (*os.File, error) {
	flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if create {
		flags |= unix.O_CREAT
	}
	fd, err := unix.Open(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, errors.New("unsafe backup lock")
	}
	return f, nil
}
func lockFile(ctx context.Context, f *os.File) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			return err
		}
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func EnablePublication(root string) error {
	f, err := openLock(filepath.Join(root, publicationFile), true)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = lockFile(context.Background(), f); err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() == 0 {
		return bump(f)
	}
	return nil
}
func bump(f *os.File) error {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return err
	}
	_, err := f.WriteAt([]byte(hex.EncodeToString(b[:])), 0)
	return err
}
func FreezePublication(ctx context.Context, root string) (func(), error) {
	f, err := openLock(filepath.Join(root, publicationFile), false)
	if err != nil {
		return nil, err
	}
	if err = lockFile(ctx, f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
func PublicationEpoch(root string) (string, error) {
	f, err := openLock(filepath.Join(root, publicationFile), false)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw := make([]byte, 64)
	_, err = io.ReadFull(f, raw)
	if err != nil {
		return "", err
	}
	if _, err = hex.DecodeString(string(raw)); err != nil {
		return "", err
	}
	return string(raw), nil
}

// Publish serializes durable file replacement/deletion, not packet processing.
// No store mutex is acquired by the backup reader while this lock is held.
func Publish(destination string, mutation func() error) error {
	dir, err := filepath.Abs(filepath.Dir(destination))
	if err != nil {
		return err
	}
	for {
		f, e := openLock(filepath.Join(dir, publicationFile), false)
		if e == nil {
			defer f.Close()
			if e = lockFile(context.Background(), f); e != nil {
				return e
			}
			if e = bump(f); e != nil {
				return e
			}
			return mutation()
		}
		if !os.IsNotExist(e) {
			return e
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return mutation()
		}
		dir = parent
	}
}

// AcquireProcessLock reserves an installation-local role until file.Close.
func AcquireProcessLock(root, name string) (*os.File, error) {
	if filepath.Base(name) != name {
		return nil, errors.New("invalid lock name")
	}
	f, e := openLock(filepath.Join(root, name), true)
	if e != nil {
		return nil, e
	}
	e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}

func copyOwnership(source, target string) error {
	info, e := os.Stat(source)
	if e != nil {
		return e
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("cannot determine data owner")
	}
	return filepath.WalkDir(target, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		return os.Chown(path, int(stat.Uid), int(stat.Gid))
	})
}

// AcquireInstallationLock uses a sibling StateDirectory which is never swapped
// by restore. All runtime owners hold a shared lock before touching data.
func AcquireInstallationLock(data string, exclusive bool) (*os.File, error) {
	dir, e := installationControl(data)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	f, e := openLock(filepath.Join(dir, "installation.lock"), true)
	if e != nil {
		return nil, e
	}
	mode := unix.LOCK_SH
	if exclusive {
		mode = unix.LOCK_EX
	}
	if e = unix.Flock(int(f.Fd()), mode|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("stop server and workers before restore; installation busy")
	}
	if !exclusive {
		if _, e = os.Lstat(filepath.Join(dir, "restore-pending")); !os.IsNotExist(e) {
			f.Close()
			return nil, errors.New("interrupted restore requires offline rollback")
		}
	}
	return f, nil
}
