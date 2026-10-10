package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotPinnedGeneration(t *testing.T) {
	root := t.TempDir()
	if err := EnablePublication(root); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "identities.json")
	os.WriteFile(p, []byte("old"), 0600)
	part := &FileParticipant{Root: root, Prefix: "data", Required: []string{"identities.json"}, Include: func(path string, kind Kind) bool { return path == "identities.json" }}
	c := NewCoordinator(root, []Participant{part}, "test")
	s, err := c.Capture(context.Background(), Full)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	tmp := filepath.Join(root, "next")
	os.WriteFile(tmp, []byte("new"), 0600)
	if err = Publish(p, func() error { return os.Rename(tmp, p) }); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Dir, "data/identities.json"))
	if err != nil || string(raw) != "old" {
		t.Fatalf("snapshot changed %q %v", raw, err)
	}
	if len(s.Manifest.Entries) != 1 || s.Manifest.Entries[0].Size != 3 {
		t.Fatal("wrong manifest")
	}
}
func TestSnapshotConfigScope(t *testing.T) {
	root := t.TempDir()
	EnablePublication(root)
	os.WriteFile(filepath.Join(root, "identities.json"), []byte("state"), 0600)
	os.WriteFile(filepath.Join(root, "history.json"), []byte("telemetry"), 0600)
	part := &FileParticipant{Root: root, Prefix: "data", Include: func(path string, kind Kind) bool {
		return path == "identities.json" || kind == Full && path == "history.json"
	}}
	c := NewCoordinator(root, []Participant{part}, "test")
	s, e := c.Capture(context.Background(), Config)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Release()
	if len(s.Manifest.Entries) != 1 {
		t.Fatal("history included")
	}
}
func TestSnapshotDeadline(t *testing.T) {
	root := t.TempDir()
	EnablePublication(root)
	release, err := FreezePublication(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	c := NewCoordinator(root, nil, "test")
	if _, err = c.Capture(ctx, Full); err == nil {
		t.Fatal("deadline ignored")
	}
}
func TestSnapshotCredentials(t *testing.T) {
	root := t.TempDir()
	EnablePublication(root)
	c := NewCoordinator(root, []Participant{&FileParticipant{Root: root, Prefix: "data", Required: []string{"missing.key"}, Include: func(string, Kind) bool { return true }}}, "test")
	if _, err := c.Capture(context.Background(), Full); err == nil {
		t.Fatal("missing secret accepted")
	}
}

type timedBackupParticipant struct {
	*FileParticipant
	elapsed time.Duration
}

func (p *timedBackupParticipant) Pin(ctx context.Context, k Kind, dir string) (func() error, error) {
	start := time.Now()
	release, e := p.FileParticipant.Pin(ctx, k, dir)
	p.elapsed = time.Since(start)
	return release, e
}
func TestSnapshotRepresentativeDataset(t *testing.T) {
	root := t.TempDir()
	EnablePublication(root)
	for i := 0; i < 3000; i++ {
		if e := os.WriteFile(filepath.Join(root, fmt.Sprintf("%04d.json", i)), []byte(`{"sample":1}`), 0600); e != nil {
			t.Fatal(e)
		}
	}
	p := &timedBackupParticipant{FileParticipant: &FileParticipant{Root: root, Prefix: "data", Include: func(path string, _ Kind) bool { return filepath.Ext(path) == ".json" }}}
	c := NewCoordinator(root, []Participant{p}, "test")
	snap, e := c.Capture(context.Background(), Full)
	if e != nil {
		t.Fatal(e)
	}
	defer snap.Release()
	if len(snap.Manifest.Entries) != 3000 || p.elapsed > time.Second {
		t.Fatal("snapshot barrier exceeded target", p.elapsed)
	}
	t.Logf("3000 durable records pinned in %s", p.elapsed)
}

func TestSnapshotSystemdStateDirectoryAlias(t *testing.T) {
	parent := t.TempDir()
	actual := filepath.Join(parent, "private/data")
	os.MkdirAll(actual, 0700)
	alias := filepath.Join(parent, "data")
	if e := os.Symlink(actual, alias); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(actual, "identities.json"), []byte("state"), 0600)
	EnablePublication(alias)
	c := NewCoordinator(alias, []Participant{&FileParticipant{Root: alias, Prefix: "data", Required: []string{"identities.json"}, Include: func(path string, _ Kind) bool { return path == "identities.json" }}}, "test")
	snap, e := c.Capture(context.Background(), Full)
	if e != nil {
		t.Fatal(e)
	}
	defer snap.Release()
	if len(snap.Manifest.Entries) != 1 {
		t.Fatal("state alias not traversed")
	}
}
