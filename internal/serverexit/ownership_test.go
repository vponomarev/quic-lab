//go:build linux

package serverexit

import (
	"context"
	"os"
	"path/filepath"
	"quiclab/internal/awg"
	"testing"
)

func TestStartOwnedLocksSeparateLegacyDirectory(t *testing.T) {
	dir, legacy := t.TempDir(), t.TempDir()
	os.Chmod(dir, 0700)
	os.Chmod(legacy, 0700)
	c, _ := awg.Parse(testAWG)
	lock, err := LockAWGPeer(legacy, c)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := NewCatalog(testDefinitions()[:1])
	if runtime, err := StartOwned(context.Background(), catalog, dir, legacy); err == nil {
		runtime.Close()
		t.Fatal("named exit stole legacy peer in separate directory")
	}
	lock.Close()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	runtime, err := StartOwned(context.Background(), catalog, dir, legacy, alias)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Close()
	for _, path := range []string{dir, legacy} {
		lock, err := LockAWGPeer(path, c)
		if err != nil {
			t.Fatal("lock leaked", err)
		}
		lock.Close()
	}
}

func TestAWGPeerOwnershipAcrossRuntimeRoles(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	c, _ := awg.Parse(testAWG)
	first, err := LockAWGPeer(dir, c)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := LockAWGPeer(dir, c); err == nil {
		second.Close()
		t.Fatal("duplicate role owns peer")
	}
	first.Close()
	second, err := LockAWGPeer(dir, c)
	if err != nil {
		t.Fatal(err)
	}
	second.Close()
}
