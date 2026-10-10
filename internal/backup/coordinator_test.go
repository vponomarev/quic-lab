package backup

import (
	"context"
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
