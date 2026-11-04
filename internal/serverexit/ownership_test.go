//go:build linux

package serverexit

import (
	"os"
	"quiclab/internal/awg"
	"testing"
)

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
