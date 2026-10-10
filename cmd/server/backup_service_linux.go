//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"quiclab/internal/admin"
	"quiclab/internal/backup"
)

func initBackupStorage(dir string) (func(), error) {
	gate, e := backup.AcquireInstallationLock(dir, false)
	if e != nil {
		return nil, e
	}
	success := false
	defer func() {
		if !success {
			gate.Close()
		}
	}()
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	f, e := backup.AcquireProcessLock(dir, ".server-owner.lock")
	if e != nil {
		return nil, e
	}
	if e = backup.EnablePublication(dir); e != nil {
		f.Close()
		return nil, e
	}
	success = true
	return func() { f.Close(); gate.Close() }, nil
}
func startServerBackups(ctx context.Context, ui *admin.Web, opts serverConfig) (func(), error) {
	cfg := ui.Config
	raw, e := json.MarshalIndent(opts, "", "  ")
	if e != nil {
		return nil, e
	}
	startup := filepath.Join(cfg.DataDir, ".backup-server.json")
	if e = os.WriteFile(startup, raw, 0600); e != nil {
		return nil, e
	}
	sources := map[string]string{"config/server.json": startup, "config/cert.pem": opts.Cert, "config/key.pem": opts.Key}
	if opts.ClientCA != "" {
		sources["config/client-ca.pem"] = opts.ClientCA
	}
	if cfg.Transit != nil {
		source := filepath.Join(os.Getenv("CREDENTIALS_DIRECTORY"), "backup-transit.json")
		if os.Getenv("CREDENTIALS_DIRECTORY") == "" {
			source = "/etc/quic-lab/transit.json"
		}
		sources["config/transit.json"] = source
	}
	participants := []backup.Participant{admin.NewBackupParticipant(cfg, sources, validateBackupServerConfig)}
	if ui.Capture != nil {
		participants = append(participants, ui.Capture.BackupParticipant())
	}
	coordinator := backup.NewCoordinator(cfg.DataDir, participants, backup.BuildVersion)
	quota := cfg.BackupMiB
	if quota == 0 {
		quota = 2048
	}
	manager, e := backup.NewManager(filepath.Join(cfg.DataDir, "backups"), coordinator, int64(quota)<<20)
	if e != nil {
		return nil, e
	}
	socket := filepath.Join(cfg.DataDir, "backups/control.sock")
	if info, e := os.Lstat(socket); e == nil {
		if info.Mode()&os.ModeSocket == 0 {
			manager.Close()
			return nil, errors.New("unsafe backup socket path")
		}
		if e = os.Remove(socket); e != nil {
			manager.Close()
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		manager.Close()
		return nil, e
	}
	listener, e := net.Listen("unix", socket)
	if e != nil {
		manager.Close()
		return nil, e
	}
	if e = os.Chmod(socket, 0600); e != nil {
		listener.Close()
		manager.Close()
		return nil, e
	}
	child, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); backup.ServeLocal(child, listener, manager) }()
	ui.SetBackupManager(manager)
	return func() { cancel(); listener.Close(); <-done; manager.Close() }, nil
}
