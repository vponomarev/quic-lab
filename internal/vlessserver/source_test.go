package vlessserver

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestAdmissionSourceMetadata(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	stop, err := ServeAdmission(context.Background(), dir, func(ctx context.Context, id, uuid string) (time.Duration, error) {
		if AdmissionSource(ctx) != "198.51.100.1:54321" {
			t.Errorf("source=%q", AdmissionSource(ctx))
		}
		return time.Second, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, err = RequestAdmission(context.Background(), dir, "device", "11111111-1111-4111-8111-111111111111", "198.51.100.1:54321"); err != nil {
		t.Fatal(err)
	}
	if _, err = RequestAdmission(context.Background(), dir, "device", "11111111-1111-4111-8111-111111111111", "spoof.example:443"); err == nil {
		t.Fatal("accepted nonnumeric source")
	}
}
