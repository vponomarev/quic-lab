package vlessserver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"quiclab/internal/vless"
	"testing"
	"time"
)

type fakeManagedServer struct {
	clients []vless.ServerClient
	revoked map[string]bool
	closed  bool
}

func (f *fakeManagedServer) SetClients(_ context.Context, c []vless.ServerClient) error {
	f.clients = c
	return nil
}
func (f *fakeManagedServer) Revoke(id string) { f.revoked[id] = true }
func (f *fakeManagedServer) Close() error     { f.closed = true; return nil }
func workerFixture(t *testing.T) (*Worker, *[]*fakeManagedServer) {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	w, e := OpenWorker(context.Background(), dir, func(context.Context, string) (time.Duration, error) { return time.Second, nil })
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { w.Close() })
	var list []*fakeManagedServer
	w.start = func(context.Context, vless.ServerOptions) (managedServer, error) {
		f := &fakeManagedServer{revoked: map[string]bool{}}
		list = append(list, f)
		return f, nil
	}
	return w, &list
}
func sampleSnapshot() Snapshot {
	return Snapshot{Revision: 1, Config: realityConfig(), Devices: []Device{{ID: "device-one", UUID: "11111111-1111-4111-8111-111111111111", Expires: time.Now().Add(time.Hour)}, {ID: "device-two", UUID: "22222222-2222-4222-8222-222222222222", Expires: time.Now().Add(time.Hour)}}}
}
func TestWorkerRevisionContract(t *testing.T) {
	w, list := workerFixture(t)
	s := sampleSnapshot()
	ctx := context.Background()
	if e := w.Apply(ctx, s); e != nil {
		t.Fatal(e)
	}
	if e := w.Apply(ctx, s); e != nil || len(*list) != 1 {
		t.Fatal("idempotence failed")
	}
	conflict := s
	conflict.Config.Endpoint = "other.example:443"
	if e := w.Apply(ctx, conflict); e == nil {
		t.Fatal("revision conflict accepted")
	}
	stale := s
	stale.Revision = 0
	if e := w.Apply(ctx, stale); e == nil {
		t.Fatal("stale revision accepted")
	}
	s.Revision++
	s.Devices = s.Devices[1:]
	if e := w.Apply(ctx, s); e != nil {
		t.Fatal(e)
	}
	if len(*list) != 1 || (*list)[0].closed || !(*list)[0].revoked["11111111-1111-4111-8111-111111111111"] {
		t.Fatal("targeted revoke restarted others")
	}
}
func TestWorkerRevokeSurvivesApplyFailureAndRestart(t *testing.T) {
	w, _ := workerFixture(t)
	s := sampleSnapshot()
	ctx := context.Background()
	if e := w.Apply(ctx, s); e != nil {
		t.Fatal(e)
	}
	s.Revision++
	s.Devices = s.Devices[1:]
	s.Config.Listen = "0.0.0.0:9443"
	w.start = func(context.Context, vless.ServerOptions) (managedServer, error) {
		return nil, errors.New("bind secret-error")
	}
	if e := w.Apply(ctx, s); e == nil {
		t.Fatal("bind failure acknowledged")
	}
	w.Close()
	next, e := OpenWorker(ctx, w.dir, func(context.Context, string) (time.Duration, error) { return time.Second, nil })
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	next.start = func(context.Context, vless.ServerOptions) (managedServer, error) {
		return &fakeManagedServer{revoked: map[string]bool{}}, nil
	}
	old := sampleSnapshot()
	old.Revision = 3
	if e := next.Apply(ctx, old); e == nil {
		t.Fatal("revoked credential resurrected")
	}
	if e := next.Apply(ctx, s); e != nil {
		t.Fatal(e)
	}
	st, e := os.Stat(filepath.Join(w.dir, "vless-state.json"))
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("state not private")
	}
}
func TestWorkerWriteFailureClosesRuntime(t *testing.T) {
	w, list := workerFixture(t)
	s := sampleSnapshot()
	if e := w.Apply(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	w.persist = func(workerRecord) error { return errors.New("write failed") }
	s.Revision++
	s.Devices = s.Devices[1:]
	if e := w.Apply(context.Background(), s); e == nil || !(*list)[0].closed {
		t.Fatal("failed write left active runtime")
	}
}
func TestWorkerInvalidConfigPreservesCurrent(t *testing.T) {
	w, list := workerFixture(t)
	s := sampleSnapshot()
	if e := w.Apply(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	bad := s
	bad.Revision++
	bad.Config.Security = "invalid"
	if e := w.Apply(context.Background(), bad); e == nil {
		t.Fatal("invalid config accepted")
	}
	if len(*list) != 1 || (*list)[0].closed {
		t.Fatal("invalid revision changed runtime")
	}
}
