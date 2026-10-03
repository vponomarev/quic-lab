//go:build linux

package vlessserver

import (
	"os"
	"path/filepath"
	"syscall"
)

// systemd may expose credentials as root:root 0440 with a per-service ACL.
// Only this process's configured credential directory grants that exception.
func systemdPrivateCredential(path string, info os.FileInfo) bool {
	dir := os.Getenv("CREDENTIALS_DIRECTORY")
	if !filepath.IsAbs(dir) || filepath.Dir(filepath.Clean(path)) != filepath.Clean(dir) {
		return false
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != 0 || owner.Gid != 0 || info.Mode().Perm() != 0440 {
		return false
	}
	entry, err := os.Lstat(path)
	if err != nil || !entry.Mode().IsRegular() || !os.SameFile(entry, info) {
		return false
	}
	parent, err := os.Lstat(dir)
	if err != nil || !parent.IsDir() || parent.Mode().Perm()&0027 != 0 {
		return false
	}
	root, ok := parent.Sys().(*syscall.Stat_t)
	return ok && root.Uid == 0 && root.Gid == 0
}
