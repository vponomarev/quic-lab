package backup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreIdentity(t *testing.T) {
	s := fixture(t, Full)
	s.Manifest.ServerVersion = BuildVersion
	root := t.TempDir()
	v := &Verified{Dir: s.Dir, Manifest: s.Manifest}
	if _, e := PlanRestore(v, root); e == nil {
		t.Fatal("unknown restore component accepted")
	}
	os.MkdirAll(filepath.Join(s.Dir, "data"), 0700)
	os.Rename(filepath.Join(s.Dir, "identities.json"), filepath.Join(s.Dir, "data/identities.json"))
	v.Manifest.Entries[0].Path = "data/identities.json"
	v.Manifest.Components = []string{"data"}
	os.MkdirAll(filepath.Join(root, "var/lib/quic-lab"), 0700)
	os.WriteFile(filepath.Join(root, "var/lib/quic-lab/old"), []byte("old"), 0600)
	plan, e := PlanRestore(v, root)
	if e != nil {
		t.Fatal(e)
	}
	if e = ApplyRestore(context.Background(), plan); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(root, "var/lib/quic-lab/identities.json"))
	if e != nil || string(raw) != "private-key-data\n" {
		t.Fatal("state not restored")
	}
	if _, e = os.Stat(filepath.Join(root, "var/lib/quic-lab/old")); !os.IsNotExist(e) {
		t.Fatal("old history mixed")
	}
	if e = RollbackRestore(context.Background(), filepath.Join(plan.RollbackDir, "journal.json")); e != nil {
		t.Fatal(e)
	}
	if raw, e = os.ReadFile(filepath.Join(root, "var/lib/quic-lab/old")); e != nil || string(raw) != "old" {
		t.Fatal("rollback lost original")
	}
}
func TestRestoreVersionAndPaths(t *testing.T) {
	s := fixture(t, Config)
	v := &Verified{Dir: s.Dir, Manifest: s.Manifest}
	v.Manifest.ServerVersion = "other"
	if _, e := PlanRestore(v, t.TempDir()); e == nil {
		t.Fatal("version mismatch accepted")
	}
	v.Manifest.ServerVersion = BuildVersion
	v.Manifest.Entries[0].Path = "../../escape"
	if _, e := PlanRestore(v, t.TempDir()); e == nil {
		t.Fatal("unsafe path accepted")
	}
}
func TestRestoreRejectLiveOwner(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "var/lib/quic-lab")
	os.MkdirAll(data, 0700)
	owner, e := AcquireProcessLock(data, ".server-owner.lock")
	if e != nil {
		t.Fatal(e)
	}
	defer owner.Close()
	s := fixture(t, Full)
	os.MkdirAll(filepath.Join(s.Dir, "data"), 0700)
	os.Rename(filepath.Join(s.Dir, "identities.json"), filepath.Join(s.Dir, "data/identities.json"))
	s.Manifest.Entries[0].Path = "data/identities.json"
	s.Manifest.Components = []string{"data"}
	s.Manifest.ServerVersion = BuildVersion
	plan, e := PlanRestore(&Verified{Dir: s.Dir, Manifest: s.Manifest}, root)
	if e != nil {
		t.Fatal(e)
	}
	if e = ApplyRestore(context.Background(), plan); e == nil {
		t.Fatal("live installation replaced")
	}
}
func TestRestoreCrashRollback(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "var/lib/quic-lab")
	rollback := filepath.Join(root, "var/lib/quic-lab-restore", "0123456789abcdef0123456789abcdef")
	os.MkdirAll(filepath.Join(rollback, "old"), 0700)
	os.MkdirAll(filepath.Join(rollback, "old/data"), 0700)
	os.WriteFile(filepath.Join(rollback, "old/data/original"), []byte("state"), 0600)
	raw, _ := json.Marshal(map[string]any{"root": root, "phase": "applying", "targets": []map[string]any{{"destination": target, "name": "data", "had_old": true, "old_moved": true, "new_moved": false}}})
	os.WriteFile(filepath.Join(rollback, "journal.json"), raw, 0600)
	if e := RollbackRestore(context.Background(), filepath.Join(rollback, "journal.json")); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(target, "original")); e != nil {
		t.Fatal("interrupted old rename not restored")
	}
}
