//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"io"
	"os"
	"path/filepath"
	"quiclab/internal/admin"
	"quiclab/internal/backup"
)

func backupPassword(path string, fd int, stdin io.Reader, stderr io.Writer) ([]byte, error) {
	if path != "" && fd >= 0 {
		return nil, errors.New("choose one password source")
	}
	var reader io.Reader
	if path != "" {
		n, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e != nil {
			return nil, errors.New("password file unavailable")
		}
		f := os.NewFile(uintptr(n), "password")
		defer f.Close()
		s, e := f.Stat()
		if e != nil || !s.Mode().IsRegular() || s.Mode().Perm()&0077 != 0 {
			return nil, errors.New("password file must be private regular file (0600)")
		}
		reader = f
	} else if fd >= 0 {
		if fd == 0 {
			reader = stdin
		} else {
			n, e := unix.Dup(fd)
			if e != nil {
				return nil, errors.New("password descriptor unavailable")
			}
			f := os.NewFile(uintptr(n), "password")
			defer f.Close()
			reader = f
		}
	} else {
		f, ok := stdin.(*os.File)
		if !ok || !term.IsTerminal(int(f.Fd())) {
			return nil, errors.New("noninteractive use requires --password-file or --password-fd")
		}
		fmt.Fprint(stderr, "Backup password: ")
		raw, e := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stderr)
		if e != nil || len(raw) == 0 || len(raw) > 1024 {
			clear(raw)
			return nil, errors.New("invalid password input")
		}
		return raw, nil
	}
	raw, e := io.ReadAll(io.LimitReader(reader, 1026))
	if e != nil {
		return nil, errors.New("password source unavailable")
	}
	raw = bytes.TrimSuffix(raw, []byte("\n"))
	raw = bytes.TrimSuffix(raw, []byte("\r"))
	if len(raw) == 0 || len(raw) > 1024 {
		clear(raw)
		return nil, errors.New("invalid password length")
	}
	return raw, nil
}
func runBackup(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: backup create|inspect|verify|restore|rollback")
		return 2
	}
	command := args[0]
	if command != "create" && command != "inspect" && command != "verify" && command != "restore" && command != "rollback" {
		fmt.Fprintln(stderr, "unknown backup command")
		return 2
	}
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	kind := fs.String("type", "config", "config or full")
	output := fs.String("output", "", "output archive (must not exist)")
	input := fs.String("input", "", "encrypted archive")
	socket := fs.String("socket", "/var/lib/quic-lab/backups/control.sock", "local backup socket")
	passwordFile := fs.String("password-file", "", "private password file")
	passwordFD := fs.Int("password-fd", -1, "password descriptor")
	machine := fs.Bool("json", false, "JSON output")
	root := fs.String("root", "/", "installed root for offline restore")
	yes := fs.Bool("yes", false, "apply verified restore")
	journal := fs.String("journal", "", "rollback journal")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || (*kind != "config" && *kind != "full") || *passwordFD < -1 || (command == "create" && *output == "") || (command != "create" && command != "rollback" && *input == "") || (command == "rollback" && *journal == "") {
		fmt.Fprintln(stderr, "invalid backup arguments")
		return 2
	}
	if command == "rollback" {
		if e := backup.RollbackRestore(ctx, *journal); e != nil {
			fmt.Fprintln(stderr, e)
			return 4
		}
		fmt.Fprintln(stdout, "Rollback completed; services remain stopped")
		return 0
	}
	password, e := backupPassword(*passwordFile, *passwordFD, stdin, stderr)
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 2
	}
	defer clear(password)
	if command == "create" {
		fmt.Fprintln(stderr, "Creating encrypted backup…")
		result, e := backup.CreateLocal(ctx, *socket, backup.Kind(*kind), password, *output)
		if e != nil {
			fmt.Fprintln(stderr, e)
			if errors.Is(e, backup.ErrBusy) || errors.Is(e, backup.ErrUnavailable) || errors.Is(e, os.ErrNotExist) {
				return 3
			}
			return 4
		}
		if *machine {
			if json.NewEncoder(stdout).Encode(result) != nil {
				return 4
			}
		} else {
			fmt.Fprintf(stdout, "%s\n%d bytes SHA256 %s\n", result.Path, result.SizeBytes, result.SHA256)
		}
		return 0
	}
	file, e := os.Open(*input)
	if e != nil {
		fmt.Fprintln(stderr, "backup input unavailable")
		return 4
	}
	defer file.Close()
	staging, e := os.MkdirTemp("", "quic-backup-verify-")
	if e != nil {
		return 4
	}
	defer os.RemoveAll(staging)
	verified, e := backup.Verify(ctx, file, password, staging, backup.Limits{MaxFiles: 100000, MaxBytes: 64 << 30})
	if e != nil {
		fmt.Fprintln(stderr, "backup authentication or validation failed")
		return 2
	}
	defer verified.Release()
	if e = admin.ValidateBackup(verified, command == "restore"); e != nil {
		fmt.Fprintln(stderr, e)
		return 2
	}
	if command == "restore" {
		plan, e := backup.PlanRestore(verified, *root)
		if e != nil {
			fmt.Fprintln(stderr, e)
			return 2
		}
		fmt.Fprintln(stderr, "Restore replaces configuration and application data. Later key revocations may be lost. Services must be stopped.")
		if !*yes {
			json.NewEncoder(stdout).Encode(plan)
			fmt.Fprintln(stderr, "Preview only; repeat with --yes to apply")
			return 0
		}
		if e = backup.ApplyRestore(ctx, plan); e != nil {
			fmt.Fprintln(stderr, e)
			fmt.Fprintf(stderr, "Keep services stopped. Rollback: backup rollback --journal %q\n", filepath.Join(plan.RollbackDir, "journal.json"))
			return 4
		}
		if *machine {
			json.NewEncoder(stdout).Encode(plan)
		} else {
			fmt.Fprintln(stdout, "Restore completed; services remain stopped. Rollback journal:", filepath.Join(plan.RollbackDir, "journal.json"))
		}
		return 0
	}
	if *machine || command == "inspect" {
		if json.NewEncoder(stdout).Encode(verified.Manifest) != nil {
			return 4
		}
	} else {
		fmt.Fprintln(stdout, "Backup verified")
	}
	return 0
}
