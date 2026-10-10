//go:build linux

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupCreateCLI(t *testing.T) {
	for _, args := range [][]string{{"create", "--type", "full", "--output", "/tmp/not-created.age"}, {"create", "--type", "full", "--password", "secret"}, {"invalid"}} {
		var out, err bytes.Buffer
		code := runBackup(context.Background(), args, strings.NewReader(""), &out, &err)
		if code != 2 || out.Len() != 0 || strings.Contains(err.String(), "secret") {
			t.Fatalf("invalid args: %d %s", code, err.String())
		}
	}
}
func TestBackupPasswordPermissions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "password")
	os.WriteFile(p, []byte("never-log-this\n"), 0644)
	var out, err bytes.Buffer
	code := runBackup(context.Background(), []string{"create", "--output", filepath.Join(t.TempDir(), "x.age"), "--password-file", p}, strings.NewReader(""), &out, &err)
	if code != 2 || strings.Contains(err.String(), "never-log-this") {
		t.Fatal("unsafe password source accepted")
	}
}

func TestBackupVerificationStagingCrashCleanup(t *testing.T) {
	base := filepath.Join(t.TempDir(), "private")
	stage, cleanup, e := backupVerificationStaging(base)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(stage, "key"), []byte("private"), 0600); e != nil {
		t.Fatal(e)
	}
	// Simulate a dead process: its lock is released, but plaintext remains.
	old := stage + "-interrupted"
	if e = os.Rename(stage, old); e != nil {
		t.Fatal(e)
	}
	cleanup()
	stage, cleanup, e = backupVerificationStaging(base)
	if e != nil {
		t.Fatal(e)
	}
	defer cleanup()
	if _, e = os.Stat(old); !os.IsNotExist(e) {
		t.Fatal("plaintext staging survived restart")
	}
}
