package windowsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeFirstRunRequiresBothStoresAbsent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	base, err := NewEncryptedStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := NewSettingsFileStore(base)
	d, err := store.Load(context.Background())
	if err != nil || d != nil {
		t.Fatalf("fresh store: %v, %v", d, err)
	}
	if err = os.WriteFile(filepath.Join(dir, "engine.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Load(context.Background()); err == nil {
		t.Fatal("bad legacy treated as first run")
	}
	if err = os.Remove(filepath.Join(dir, "engine.json")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "settings-v1.dpapi"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Load(context.Background()); err == nil {
		t.Fatal("bad encrypted store treated as first run")
	}
}

func TestNativeSettingsMigrationAndEncryptedRoundtrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	base, e := NewEncryptedStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	store := NewSettingsFileStore(base)
	ctx := context.Background()
	old := editorSettings()
	raw, _ := json.Marshal(old)
	if e = os.WriteFile(filepath.Join(dir, "engine.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	d, e := store.Load(ctx)
	if e != nil || d == nil || d.Revision != 1 {
		t.Fatal("migration", e)
	}
	encrypted, e := os.ReadFile(filepath.Join(dir, "settings-v1.dpapi"))
	if e != nil || bytes.Contains(encrypted, []byte(old.Key)) {
		t.Fatal("plaintext")
	}
	d.Revision = 2
	d.Settings.Name = "edited"
	if e = store.Save(ctx, *d); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "engine.json"), []byte("broken legacy"), 0600)
	d, e = store.Load(ctx)
	if e != nil || d.Revision != 2 || d.Settings.Name != "edited" || d.Settings.Key != old.Key {
		t.Fatal("legacy overwrote edited settings", e)
	}
	os.WriteFile(filepath.Join(dir, "settings-v1.dpapi"), []byte("corrupt"), 0600)
	if _, e = store.Load(ctx); e == nil {
		t.Fatal("corrupt new storage ignored")
	}
}
func TestNativeSettingsCancelledSavePreservesDisk(t *testing.T) {
	base, e := NewEncryptedStore(filepath.Join(t.TempDir(), "store"))
	if e != nil {
		t.Fatal(e)
	}
	s := NewSettingsFileStore(base)
	d := SettingsDocument{Revision: 1, Settings: editorSettings()}
	if e = s.Save(context.Background(), d); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d.Revision = 2
	if s.Save(ctx, d) == nil {
		t.Fatal("cancel ignored")
	}
	got, e := s.Load(context.Background())
	if e != nil || got.Revision != 1 {
		t.Fatal("old data lost")
	}
}
