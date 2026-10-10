package debugcapture

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"quiclab/internal/backup"
	"testing"
)

func TestSnapshotCaptureBoundary(t *testing.T) {
	s := testSession()
	defer s.cancel()
	first := section(101)
	record := block(6, make([]byte, 20))
	first = append(first, record...)
	s.emit(first)
	m := &Manager{sessions: map[string]*Session{s.ID: s}}
	p := m.BackupParticipant()
	dir := t.TempDir()
	release, err := p.Pin(context.Background(), backup.Full, dir)
	if err != nil {
		t.Fatal(err)
	}
	if release != nil {
		defer release()
	}
	s.emit(block(6, make([]byte, 24)))
	if err = p.(interface {
		Transform(context.Context, backup.Kind, string) error
	}).Transform(context.Background(), backup.Full, dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "data/downloads/capture/id.pcapng"))
	if err != nil || !bytes.Equal(raw, first) {
		t.Fatalf("bad boundary %q %v", raw, err)
	}
}
