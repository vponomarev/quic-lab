//go:build linux

package vlessserver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPrivateSystemdCredential(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned systemd credential fixture")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0750); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(key, []byte("fixture"), 0440); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", dir)
	if _, err := readMaterial(key, true); err != nil {
		t.Fatal("systemd credential rejected", err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", dir+"-other")
	if _, err := readMaterial(key, true); err == nil {
		t.Fatal("ordinary group-readable key accepted")
	}
	t.Setenv("CREDENTIALS_DIRECTORY", dir)
	if err := os.Chmod(key, 0444); err != nil {
		t.Fatal(err)
	}
	if _, err := readMaterial(key, true); err == nil {
		t.Fatal("world-readable credential accepted")
	}
	if err := os.Chmod(key, 0440); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0770); err != nil {
		t.Fatal(err)
	}
	if _, err := readMaterial(key, true); err == nil {
		t.Fatal("group-writable credential directory accepted")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(key, 12345, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := readMaterial(key, true); err == nil {
		t.Fatal("non-root credential accepted")
	}
}
