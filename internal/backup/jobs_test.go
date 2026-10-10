package backup

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testManager(t *testing.T, quota int64) *Manager {
	t.Helper()
	root := t.TempDir()
	EnablePublication(root)
	os.WriteFile(filepath.Join(root, "identities.json"), []byte("state"), 0600)
	p := &FileParticipant{Root: root, Prefix: "data", Include: func(s string, _ Kind) bool { return s == "identities.json" }}
	c := NewCoordinator(root, []Participant{p}, "test")
	m, e := NewManager(filepath.Join(root, "backups"), c, quota)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Close() })
	return m
}
func waitJob(t *testing.T, m *Manager, id string) Job {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		j, e := m.Get(id)
		if e != nil {
			t.Fatal(e)
		}
		if j.State == "ready" || j.State == "error" {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job timed out")
	return Job{}
}
func TestJobsLimits(t *testing.T) {
	m := testManager(t, 1<<20)
	j, e := m.Start(context.Background(), Config, []byte("secret"), "one")
	if e != nil {
		t.Fatal(e)
	}
	same, e := m.Start(context.Background(), Config, []byte("secret"), "one")
	if e != nil || same.ID != j.ID {
		t.Fatal("duplicate started")
	}
	if _, e = m.Start(context.Background(), Full, []byte("secret"), "other"); e == nil {
		t.Fatal("parallel job accepted")
	}
	if j = waitJob(t, m, j.ID); j.State != "ready" || j.ExpiresAt.Sub(j.CreatedAt) != 24*time.Hour {
		t.Fatalf("bad job %+v", j)
	}
	for i := 0; i < 2; i++ {
		x, e := m.Start(context.Background(), Full, []byte("secret"), strings.Repeat("x", i+1))
		if e != nil {
			t.Fatal(e)
		}
		if waitJob(t, m, x.ID).State != "ready" {
			t.Fatal("job failed")
		}
	}
	if _, e = m.Start(context.Background(), Full, []byte("secret"), "fourth"); e == nil {
		t.Fatal("limit ignored")
	}
	entries, _ := os.ReadDir(m.root)
	for _, f := range entries {
		if strings.HasSuffix(f.Name(), ".json") {
			raw, _ := os.ReadFile(filepath.Join(m.root, f.Name()))
			if strings.Contains(string(raw), "secret") {
				t.Fatal("password persisted")
			}
		}
	}
}
func TestJobsDownloadLease(t *testing.T) {
	m := testManager(t, 1<<20)
	j, e := m.Start(context.Background(), Config, []byte("pass"), "download")
	if e != nil {
		t.Fatal(e)
	}
	waitJob(t, m, j.ID)
	r, info, e := m.Open(j.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Delete(j.ID); e != nil {
		t.Fatal(e)
	}
	raw, e := io.ReadAll(r)
	r.Close()
	if e != nil || int64(len(raw)) != info.SizeBytes {
		t.Fatal("download interrupted")
	}
	if _, e = m.Get(j.ID); e == nil {
		t.Fatal("deleted job visible")
	}
}
func TestJobsQuota(t *testing.T) {
	m := testManager(t, 1)
	j, e := m.Start(context.Background(), Config, []byte("pass"), "quota")
	if e != nil {
		t.Fatal(e)
	}
	j = waitJob(t, m, j.ID)
	if j.State != "error" {
		t.Fatal("quota ignored")
	}
	if _, e := os.Stat(filepath.Join(m.root, j.ID+".age")); !os.IsNotExist(e) {
		t.Fatal("partial published")
	}
}

func TestJobsCrashCleanup(t *testing.T) {
	m := testManager(t, 1<<20)
	m.Close()
	partial := filepath.Join(m.root, ".partial-archive-crashed")
	os.WriteFile(partial, []byte("partial"), 0600)
	snap, e := os.MkdirTemp(m.coordinator.root, ".backup-snapshot-")
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(snap, "secret"), []byte("secret"), 0600)
	again, e := NewManager(m.root, m.coordinator, 1<<20)
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	if _, e = os.Stat(partial); !os.IsNotExist(e) {
		t.Fatal("partial archive retained")
	}
	if _, e = os.Stat(snap); !os.IsNotExist(e) {
		t.Fatal("plaintext snapshot retained after crash")
	}
}
