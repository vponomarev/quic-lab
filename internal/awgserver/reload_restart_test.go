//go:build linux

package awgserver

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

func TestReloadRecoversOwnedStaleSocket(t *testing.T) {
	w := deviceWorker(t)
	path := reloadPath(w.Dir)
	stale, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	stale.SetUnlinkOnClose(false)
	stale.Close()
	os.Chmod(path, 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, stop, e := w.listenReload(ctx)
	if e != nil {
		t.Fatal("owned stale socket prevented restart", e)
	}
	defer stop()
	c, e := net.Dial("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	c.Close()
}
func TestReloadRejectsLiveAndForeignPaths(t *testing.T) {
	w := deviceWorker(t)
	path := reloadPath(w.Dir)
	live, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	defer live.Close()
	if e = os.Chmod(path, 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, stop, e := w.listenReload(ctx); e == nil {
		stop()
		t.Fatal("replaced live listener")
	}
	c, e := net.Dial("unix", path)
	if e != nil {
		t.Fatal("live listener removed", e)
	}
	c.Close()
	live.Close()
	if e = os.WriteFile(path, []byte("foreign regular file"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, stop, e := w.listenReload(ctx); e == nil {
		stop()
		t.Fatal("removed foreign path")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "foreign regular file" {
		t.Fatal("foreign path changed")
	}
}

func TestReloadExclusiveWorkerOwnership(t *testing.T) {
	w := deviceWorker(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, stop, e := w.listenReload(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	before, e := os.Lstat(reloadPath(w.Dir))
	if e != nil {
		t.Fatal(e)
	}
	other := &Worker{Dir: w.Dir}
	if _, otherStop, e := other.listenReload(ctx); e == nil {
		otherStop()
		t.Fatal("second worker acquired control ownership")
	}
	after, e := os.Lstat(reloadPath(w.Dir))
	if e != nil || !os.SameFile(before, after) {
		t.Fatal("ownership contender replaced active socket")
	}
}
func TestReloadRejectsForeignOwnedStaleSocket(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("different owner fixture requires root")
	}
	w := deviceWorker(t)
	path := reloadPath(w.Dir)
	stale, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	stale.SetUnlinkOnClose(false)
	stale.Close()
	defer os.Remove(path)
	if e = os.Chmod(path, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Chown(path, os.Geteuid()+1, -1); e != nil {
		t.Fatal(e)
	}
	before, e := os.Lstat(path)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, stop, e := w.listenReload(ctx); e == nil {
		stop()
		t.Fatal("foreign-owned stale socket replaced")
	}
	after, e := os.Lstat(path)
	if e != nil || !os.SameFile(before, after) {
		t.Fatal("foreign-owned socket removed")
	}
}

func TestReloadStopClosesPendingRequest(t *testing.T) {
	w := deviceWorker(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, stop, e := w.listenReload(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	c, e := net.Dial("unix", reloadPath(w.Dir))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if _, e = c.Write([]byte("reload\n")); e != nil {
		t.Fatal(e)
	}
	time.Sleep(20 * time.Millisecond)
	stop()
	c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	var b [8]byte
	if _, e = c.Read(b[:]); !errors.Is(e, io.EOF) {
		t.Fatal("stopped listener retained accepted request", e)
	}
}
