package cellgeo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateBackupReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cells #1.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateBackup(path); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("snapshot was modified")
	}
	if err = ValidateBackup(path + "-missing"); !os.IsNotExist(err) {
		t.Fatalf("missing file: %v", err)
	}
}
