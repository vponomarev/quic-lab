package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPublicationSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := EnablePublication(root); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "state.json")
	if err := os.WriteFile(p, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	release, err := FreezePublication(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- Publish(p, func() error { return os.WriteFile(p, []byte("new"), 0600) }) }()
	select {
	case <-done:
		t.Fatal("writer crossed snapshot barrier")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("writer remained blocked")
	}
}
func TestPublicationDeadline(t *testing.T) {
	root := t.TempDir()
	if err := EnablePublication(root); err != nil {
		t.Fatal(err)
	}
	release, err := FreezePublication(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = FreezePublication(ctx, root)
	if err == nil {
		t.Fatal("missing deadline")
	}
}
func TestPublicationEpoch(t *testing.T) {
	root := t.TempDir()
	if err := EnablePublication(root); err != nil {
		t.Fatal(err)
	}
	a, err := PublicationEpoch(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = Publish(filepath.Join(root, "x"), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	b, err := PublicationEpoch(root)
	if err != nil || a == b {
		t.Fatalf("epoch did not change: %v", err)
	}
}
