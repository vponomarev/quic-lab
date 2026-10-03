//go:build linux

package awgserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAdmissionWithSystemdStateDirectorySymlink(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "private", "quic-lab")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "quic-lab")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := ServeAdmission(ctx, link, func(string) (time.Duration, error) { return AdmissionLeaseTTL, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, err := RequestAdmission(link, "device"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := RequestAdmission(link, "device"); err == nil {
		t.Fatal("accepted non-private symlink target")
	}
}
