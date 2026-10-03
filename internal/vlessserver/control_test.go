package vlessserver

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestControlAcknowledgesAppliedRevision(t *testing.T) {
	w, _ := workerFixture(t)
	stop, e := ServeControl(context.Background(), w)
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	client := &ControlClient{Dir: w.dir}
	s := sampleSnapshot()
	if e := client.Apply(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	conflict := s
	conflict.Config.Endpoint = "other.example:443"
	if e := client.Apply(context.Background(), conflict); e == nil {
		t.Fatal("failed revision acknowledged")
	}
	st, e := os.Stat(filepath.Join(w.dir, "vless-control.sock"))
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("control not private")
	}
	if duplicate, e := ServeControl(context.Background(), w); e == nil {
		duplicate()
		t.Fatal("duplicate listener accepted")
	}
	stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := client.Apply(ctx, s); e == nil {
		t.Fatal("offline worker acknowledged")
	}
}
func TestControlRejectsForeignSocketFile(t *testing.T) {
	w, _ := workerFixture(t)
	p := filepath.Join(w.dir, "vless-control.sock")
	os.WriteFile(p, []byte("keep"), 0600)
	if stop, e := ServeControl(context.Background(), w); e == nil {
		stop()
		t.Fatal("foreign file replaced")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "keep" {
		t.Fatal("foreign file modified")
	}
}
func TestAdmissionBrokerProtocol(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	stop, e := ServeAdmission(context.Background(), dir, func(_ context.Context, id, uuid string) (time.Duration, error) {
		if id != "device-one" {
			return 0, errApply
		}
		return time.Second, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	if ttl, e := RequestAdmission(context.Background(), dir, "device-one", "11111111-1111-4111-8111-111111111111"); e != nil || ttl != time.Second {
		t.Fatal("valid device denied")
	}
	if _, e := RequestAdmission(context.Background(), dir, "device-two", "11111111-1111-4111-8111-111111111111"); e == nil {
		t.Fatal("unknown device admitted")
	}
}
func TestControlRejectsOversizedAndUnknownMessages(t *testing.T) {
	w, _ := workerFixture(t)
	stop, e := ServeControl(context.Background(), w)
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	for _, raw := range [][]byte{{0xff, 0xff, 0xff, 0xff}, {0, 0, 0, 13, '{', '"', 'u', 'n', 'k', 'n', 'o', 'w', 'n', '"', ':', '1', '}'}} {
		c, e := net.Dial("unix", filepath.Join(w.dir, "vless-control.sock"))
		if e != nil {
			t.Fatal(e)
		}
		c.SetDeadline(time.Now().Add(time.Second))
		c.Write(raw)
		var reply controlResponse
		e = readMessage(c, &reply)
		c.Close()
		if e == nil && reply.Applied {
			t.Fatal("invalid request applied")
		}
	}
}

func TestControlStatusRequiresAppliedRevision(t *testing.T) {
	w, _ := workerFixture(t)
	stop, e := ServeControl(context.Background(), w)
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	client := ControlClient{Dir: w.dir}
	s := sampleSnapshot()
	if client.CheckApplied(context.Background(), s.Revision) == nil {
		t.Fatal("unapplied worker healthy")
	}
	if e := client.Apply(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	if e := client.CheckApplied(context.Background(), s.Revision); e != nil {
		t.Fatal(e)
	}
	if client.CheckApplied(context.Background(), s.Revision+1) == nil {
		t.Fatal("old revision reported healthy")
	}
	w.Close()
	if client.CheckApplied(context.Background(), s.Revision) == nil {
		t.Fatal("closed worker healthy")
	}
}

func TestPrivateStateDirectoryAllowsSystemdAlias(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	alias := filepath.Join(t.TempDir(), "state")
	if e := os.Symlink(dir, alias); e != nil {
		t.Fatal(e)
	}
	stop, e := ServeAdmission(context.Background(), alias, func(context.Context, string, string) (time.Duration, error) { return time.Second, nil })
	if e != nil {
		t.Fatal("systemd StateDirectory alias rejected")
	}
	defer stop()
	if _, e := RequestAdmission(context.Background(), alias, "device", "11111111-1111-4111-8111-111111111111"); e != nil {
		t.Fatal(e)
	}
}
