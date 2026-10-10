package admin

import (
	"context"
	"net/url"
	"path/filepath"
	"quiclab/internal/backup"
	"strings"
	"testing"
	"time"
)

func TestBackupWebAuth(t *testing.T) {
	cfg := config(t)
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	cfg.DataDir = filepath.Dir(s.path)
	backup.EnablePublication(cfg.DataDir)
	c := backup.NewCoordinator(cfg.DataDir, []backup.Participant{NewBackupParticipant(cfg, nil)}, "test")
	m, e := backup.NewManager(filepath.Join(cfg.DataDir, "backups"), c, 1<<20)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	w := NewWeb(cfg, s)
	w.SetBackupManager(m)
	h := w.Handler()
	if r := call(h, "GET", "/backups", "", nil); r.Code != 303 {
		t.Fatal("unauthenticated page")
	}
	login := call(h, "POST", "/login", url.Values{"username": {cfg.Username}, "password": {cfg.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	if r := call(h, "POST", "/backups/create", "type=full&password=pass&repeat=pass", cookie); r.Code != 403 {
		t.Fatal("CSRF bypass")
	}
	page := call(h, "GET", "/backups", "", cookie)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Резервные копии") {
		t.Fatal("missing UI")
	}
	j, e := m.Start(context.Background(), backup.Config, []byte("pass"), "web")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 1500; i++ {
		j, _ = m.Get(j.ID)
		if j.State == "ready" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if j.State != "ready" {
		t.Fatal("job failed")
	}
	if r := call(h, "GET", "/backups/"+j.ID+"/download", "", nil); r.Code != 303 {
		t.Fatal("unauthenticated download")
	}
	r := call(h, "GET", "/backups/"+j.ID+"/download", "", cookie)
	if r.Code != 200 || r.Header().Get("Cache-Control") != "no-store" || int64(r.Body.Len()) != j.SizeBytes {
		t.Fatal("invalid download")
	}
}

func TestBackupRestoreSchema(t *testing.T) {
	cfg := config(t)
	s, e := OpenStore(cfg.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	s.Create("owner")
	backup.EnablePublication(cfg.DataDir)
	c := backup.NewCoordinator(cfg.DataDir, []backup.Participant{NewBackupParticipant(cfg, nil)}, backup.BuildVersion)
	snapshot, e := c.Capture(context.Background(), backup.Config)
	if e != nil {
		t.Fatal(e)
	}
	defer snapshot.Release()
	v := &backup.Verified{Dir: snapshot.Dir, Manifest: snapshot.Manifest}
	if e = ValidateBackup(v, false); e != nil {
		t.Fatal(e)
	}
	v.Manifest.Entries = append(v.Manifest.Entries, backup.Entry{Path: "data/admin-sessions.json"})
	if e = ValidateBackup(v, false); e == nil {
		t.Fatal("runtime sessions accepted")
	}
}
