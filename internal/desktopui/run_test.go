//go:build linux

package desktopui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSingleInstanceLockAndSymlinkRejection(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := instanceLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := instanceLock(dir); err == nil {
		other.Close()
		t.Fatal("second instance acquired lock")
	}
	lock.Close()
	lock, err = instanceLock(dir)
	if err != nil {
		t.Fatal("lock not released", err)
	}
	lock.Close()
	if err := os.Remove(filepath.Join(dir, "quic-lab-gui.lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "quic-lab-gui.lock")); err != nil {
		t.Fatal(err)
	}
	if lock, err := instanceLock(dir); err == nil {
		lock.Close()
		t.Fatal("accepted symlink lock")
	}
	if _, err := os.Stat(filepath.Join(dir, "target")); !os.IsNotExist(err) {
		t.Fatal("symlink target was created")
	}
}

func TestRunRejectsRootBeforeGraphics(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-specific guard")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if err := Run(); err == nil {
		t.Fatal("root GUI allowed")
	}
}
