package admin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"quiclab/internal/awgserver"
	"quiclab/internal/backup"
	"quiclab/internal/serverexit"
	"quiclab/internal/vlessserver"
	"testing"
	"time"
)

func TestSnapshotConcurrentRevocation(t *testing.T) {
	root := t.TempDir()
	s, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Create("backup-test")
	if err != nil {
		t.Fatal(err)
	}
	if err = backup.EnablePublication(root); err != nil {
		t.Fatal(err)
	}
	release, err := backup.FreezePublication(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.DisableDevice(u.ID) }()
	select {
	case err := <-done:
		release()
		t.Fatalf("revocation crossed barrier: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	release()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "identities.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("missing identities")
	}
	again, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if !again.state.Devices[u.ID].Disabled {
		t.Fatal("revocation lost")
	}
}

func TestBackupIncludesServerExitCredentialsAndSelection(t *testing.T) {
	s, _, d, token := updateFixture(t)
	if err := s.ConfigureServerExits(exitDefinitions()); err != nil {
		t.Fatal(err)
	}
	s.SetServerExitApply(func([]string) error { return nil })
	grantExits(t, s, d.UserID, &serverexit.Policy{Allowed: []string{"west", "east"}, Default: "west"})
	v, _ := s.DeviceServerExits(d.ID, token)
	if _, err := s.SelectServerExit(d.ID, token, "east", v.Revision); err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(s.path)
	if err := backup.EnablePublication(root); err != nil {
		t.Fatal(err)
	}
	cfg := Config{DataDir: root, ServerExits: exitDefinitions()}
	c := backup.NewCoordinator(root, []backup.Participant{NewBackupParticipant(cfg, nil)}, "test")
	snap, err := c.Capture(context.Background(), backup.Config)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Release()
	raw, err := os.ReadFile(filepath.Join(snap.Dir, "config/admin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var restored Config
	if json.Unmarshal(raw, &restored) != nil || len(restored.ServerExits) != 2 || restored.ServerExits[0].AWGConfig != exitAWG {
		t.Fatal("exit provider credentials lost")
	}
	raw, err = os.ReadFile(filepath.Join(snap.Dir, "data/identities.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state diskState
	if json.Unmarshal(raw, &state) != nil || state.Devices[d.ID].ServerExitID != "east" || state.Users[d.UserID].ServerExitPolicy.Default != "west" {
		t.Fatal("exit policy or selection lost")
	}
}

func TestSnapshotFullAndConfig(t *testing.T) {
	root := t.TempDir()
	s, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	s.Create("owner")
	os.MkdirAll(filepath.Join(root, "client-diagnostics", "device"), 0700)
	os.WriteFile(filepath.Join(root, "client-diagnostics", "device", "batch.json"), []byte("report"), 0600)
	creds := filepath.Join(t.TempDir(), "server.json")
	os.WriteFile(creds, []byte("{}"), 0600)
	cfg := Config{DataDir: root}
	part := NewBackupParticipant(cfg, map[string]string{"config/server.json": creds})
	if err = backup.EnablePublication(root); err != nil {
		t.Fatal(err)
	}
	c := backup.NewCoordinator(root, []backup.Participant{part}, "test")
	for _, kind := range []backup.Kind{backup.Config, backup.Full} {
		snap, e := c.Capture(context.Background(), kind)
		if e != nil {
			t.Fatal(e)
		}
		_, e = os.Stat(filepath.Join(snap.Dir, "data/client-diagnostics/device/batch.json"))
		if kind == backup.Config && !os.IsNotExist(e) {
			t.Fatal("history in config")
		}
		if kind == backup.Full && e != nil {
			t.Fatal("history lost")
		}
		snap.Release()
	}
}

func TestBackupUsesDurableVLESSCredentials(t *testing.T) {
	root := t.TempDir()
	s, e := OpenStore(root)
	if e != nil {
		t.Fatal(e)
	}
	creds := t.TempDir()
	cert := filepath.Join(creds, "current-cert")
	key := filepath.Join(creds, "current-key")
	os.WriteFile(cert, []byte("current-certificate"), 0600)
	os.WriteFile(key, []byte("current-key"), 0600)
	cfg := Config{DataDir: root, VLESS: &vlessserver.Config{Security: "reality"}}
	p := NewBackupParticipant(cfg, nil)
	s.state.VLESS = &vlessserver.Config{Security: "tls", TLSCertificateFile: cert, TLSKeyFile: key}
	raw, _ := json.Marshal(s.state)
	os.WriteFile(s.path, raw, 0600)
	backup.EnablePublication(root)
	c := backup.NewCoordinator(root, []backup.Participant{p}, "test")
	snap, e := c.Capture(context.Background(), backup.Config)
	if e != nil {
		t.Fatal(e)
	}
	defer snap.Release()
	b, e := os.ReadFile(filepath.Join(snap.Dir, "config/vless-cert.pem"))
	if e != nil || string(b) != "current-certificate" {
		t.Fatal("durable VLESS secret missing", e)
	}
}

func TestBackupIncludesAWGWorkerConfig(t *testing.T) {
	root := t.TempDir()
	_, e := OpenStore(root)
	if e != nil {
		t.Fatal(e)
	}
	backup.EnablePublication(root)
	cfg := Config{DataDir: root, AWG: &awgserver.Config{Interface: "ql-awg0"}}
	c := backup.NewCoordinator(root, []backup.Participant{NewBackupParticipant(cfg, nil)}, "test")
	snap, e := c.Capture(context.Background(), backup.Config)
	if e != nil {
		t.Fatal(e)
	}
	defer snap.Release()
	b, e := os.ReadFile(filepath.Join(snap.Dir, "config/awg.json"))
	if e != nil {
		t.Fatal("worker startup configuration missing", e)
	}
	var worker struct {
		DataDir string           `json:"data_dir"`
		AWG     awgserver.Config `json:"awg"`
	}
	if json.Unmarshal(b, &worker) != nil || worker.DataDir != root || worker.AWG.Interface != "ql-awg0" {
		t.Fatal("invalid worker configuration")
	}
}
