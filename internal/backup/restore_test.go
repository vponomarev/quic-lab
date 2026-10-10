package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	os.MkdirAll(filepath.Join(root, "var/lib/quic-lab-restore/.prepare-abandoned"), 0700)
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

func TestRollbackInterruptedSwapBoundaries(t *testing.T) {
	for _, private := range []bool{false, true} {
		for _, moved := range []bool{false, true} {
			t.Run(fmt.Sprintf("private=%v/failed-moved=%v", private, moved), func(t *testing.T) {
				root := t.TempDir()
				target := filepath.Join(root, "var/lib/quic-lab")
				if private {
					target = filepath.Join(root, "var/lib/private/quic-lab")
					os.MkdirAll(filepath.Dir(target), 0700)
					if e := os.Symlink(target, filepath.Join(root, "var/lib/quic-lab")); e != nil {
						t.Fatal(e)
					}
				}
				dir := filepath.Join(root, "var/lib/quic-lab-restore", "0123456789abcdef0123456789abcdef")
				os.MkdirAll(filepath.Join(dir, "old/data"), 0700)
				os.WriteFile(filepath.Join(dir, "old/data/original"), []byte("state"), 0600)
				if moved {
					os.MkdirAll(target, 0700)
					os.MkdirAll(filepath.Join(dir, "failed/data"), 0700)
					os.WriteFile(filepath.Join(dir, "failed/data/replacement"), []byte("new"), 0600)
				}
				j := restoreJournal{Root: root, Phase: "rolling_back", Targets: []restoreStep{{Destination: target, Name: "data", HadOld: true, OldMoved: true}}}
				if e := persistJournal(dir, j); e != nil {
					t.Fatal(e)
				}
				for i := 0; i < 2; i++ {
					if e := RollbackRestore(context.Background(), filepath.Join(dir, "journal.json")); e != nil {
						t.Fatal(e)
					}
				}
				if b, e := os.ReadFile(filepath.Join(target, "original")); e != nil || string(b) != "state" {
					t.Fatal("original lost", e)
				}
			})
		}
	}
}

func TestInstallationGateSurvivesMissingData(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "var/lib/quic-lab")
	os.MkdirAll(data, 0700)
	lock, e := AcquireInstallationLock(data, true)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Close()
	if e = os.Rename(data, data+"-moved"); e != nil {
		t.Fatal(e)
	}
	if f, e := AcquireInstallationLock(data, false); e == nil {
		f.Close()
		t.Fatal("runtime entered restore gap")
	}
	if _, e = os.Stat(data); !os.IsNotExist(e) {
		t.Fatal("gate recreated data")
	}
}
func TestRuntimeRejectsUnfinishedRestore(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "var/lib/quic-lab")
	dir := filepath.Join(root, "var/lib/quic-lab-restore", "0123456789abcdef0123456789abcdef")
	os.MkdirAll(dir, 0700)
	if e := persistJournal(dir, restoreJournal{Root: root, Phase: "applying"}); e != nil {
		t.Fatal(e)
	}
	dirControl, _ := installationControl(data)
	os.MkdirAll(dirControl, 0700)
	os.WriteFile(filepath.Join(dirControl, "restore-pending"), nil, 0600)
	if f, e := AcquireInstallationLock(data, false); e == nil {
		f.Close()
		t.Fatal("runtime started unfinished restore")
	}
}

func TestInstallationGateCrossProcess(t *testing.T) {
	if data := os.Getenv("QUIC_BACKUP_LOCK_TEST"); data != "" {
		f, e := AcquireInstallationLock(data, false)
		if e != nil {
			os.Exit(11)
		}
		f.Close()
		os.Exit(0)
	}
	root := t.TempDir()
	data := filepath.Join(root, "var/lib/quic-lab")
	os.MkdirAll(data, 0700)
	f, e := AcquireInstallationLock(data, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(data, data+"-old"); e != nil {
		t.Fatal(e)
	}
	child := func() error {
		c := exec.Command(os.Args[0], "-test.run=^TestInstallationGateCrossProcess$")
		c.Env = append(os.Environ(), "QUIC_BACKUP_LOCK_TEST="+data)
		return c.Run()
	}
	e = child()
	if e == nil {
		t.Fatal("child acquired gate during missing-data interval")
	}
	if x, ok := e.(*exec.ExitError); !ok || x.ExitCode() != 11 {
		t.Fatal(e)
	}
	f.Close()
	if e = child(); e != nil {
		t.Fatal("gate not released", e)
	}
}
