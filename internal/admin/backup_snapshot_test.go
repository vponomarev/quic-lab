package admin

import (
	"context"
	"os"
	"path/filepath"
	"quiclab/internal/backup"
	"testing"
	"time"
)

func TestSnapshotConcurrentRevocation(t *testing.T) {
	root := t.TempDir()
	s, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Create("backup-test")
	if err != nil {
		t.Fatal(err)
	}
	if err = backup.EnablePublication(root); err != nil {
		t.Fatal(err)
	}
	release, err := backup.FreezePublication(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.DisableDevice(u.ID) }()
	select {
	case err := <-done:
		release()
		t.Fatalf("revocation crossed barrier: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	release()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "identities.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("missing identities")
	}
	again, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if !again.state.Devices[u.ID].Disabled {
		t.Fatal("revocation lost")
	}
}

func TestSnapshotFullAndConfig(t *testing.T) {
	root := t.TempDir()
	s, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	s.Create("owner")
	os.MkdirAll(filepath.Join(root, "client-diagnostics", "device"), 0700)
	os.WriteFile(filepath.Join(root, "client-diagnostics", "device", "batch.json"), []byte("report"), 0600)
	creds := filepath.Join(t.TempDir(), "server.json")
	os.WriteFile(creds, []byte("{}"), 0600)
	cfg := Config{DataDir: root}
	part := NewBackupParticipant(cfg, map[string]string{"config/server.json": creds})
	if err = backup.EnablePublication(root); err != nil {
		t.Fatal(err)
	}
	c := backup.NewCoordinator(root, []backup.Participant{part}, "test")
	for _, kind := range []backup.Kind{backup.Config, backup.Full} {
		snap, e := c.Capture(context.Background(), kind)
		if e != nil {
			t.Fatal(e)
		}
		_, e = os.Stat(filepath.Join(snap.Dir, "data/client-diagnostics/device/batch.json"))
		if kind == backup.Config && !os.IsNotExist(e) {
			t.Fatal("history in config")
		}
		if kind == backup.Full && e != nil {
			t.Fatal("history lost")
		}
		snap.Release()
	}
}
