//go:build linux

package backup

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestCLIAtomicOutput(t *testing.T) {
	m := testManager(t, 1<<20)
	socket := filepath.Join(t.TempDir(), "backup.sock")
	ln, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	os.Chmod(socket, 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ServeLocal(ctx, ln, m)
	dest := filepath.Join(t.TempDir(), "result.age")
	result, e := CreateLocal(context.Background(), socket, Config, []byte("pass"), dest)
	if e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(dest)
	if e != nil || info.Size() != result.SizeBytes || info.Mode().Perm() != 0600 {
		t.Fatal("bad published archive")
	}
	if len(m.List()) != 0 {
		t.Fatal("CLI staging retained after acknowledgment")
	}
	before, _ := os.ReadFile(dest)
	if _, e = CreateLocal(context.Background(), socket, Config, []byte("pass"), dest); e == nil {
		t.Fatal("overwritten archive")
	}
	after, _ := os.ReadFile(dest)
	if string(before) != string(after) {
		t.Fatal("existing destination changed")
	}
}
func TestLocalPeerAuthorization(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "backup.sock")
	ln, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	os.Chmod(socket, 0666)
	if _, e = CreateLocal(context.Background(), socket, Config, []byte("pass"), filepath.Join(t.TempDir(), "x.age")); e == nil {
		t.Fatal("unsafe socket accepted")
	}
}
