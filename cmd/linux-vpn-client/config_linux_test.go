//go:build linux && !android

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReadConfigReopensPrivateReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")
	raw := `{"transport":"quic","endpoint":"198.18.0.2:443","targets":["192.0.2.2/32"]}`
	write := func(p, data string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(path, raw)
	if _, err := readConfig(path); err != nil {
		t.Fatal(err)
	}
	write(path+".new", strings.Replace(raw, ":443", ":444", 1))
	if err := os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
	if got, err := readConfig(path); err != nil || got.Endpoint != "198.18.0.2:444" {
		t.Fatalf("replacement: %+v %v", got, err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfig(path); err == nil {
		t.Fatal("public profile accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	write(path, strings.Repeat(" ", 1<<20)+raw)
	if _, err := readConfig(path); err == nil {
		t.Fatal("oversized profile accepted")
	}
	write(path, raw+` {}`)
	if _, err := readConfig(path); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	if _, err := readConfig(dir); err == nil {
		t.Fatal("directory accepted")
	}
}

func TestReadConfigRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := readConfig(path); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(250 * time.Millisecond):
		// Release the old blocking reader so the regression fails without leaking it.
		fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK, 0600)
		if err == nil {
			defer syscall.Close(fd)
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("configuration FIFO blocked reload")
	}
}
