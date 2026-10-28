package windowsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"path/filepath"
	"quiclab/internal/windowsengine"
)

type SettingsFileStore struct{ base *EncryptedStore }

func NewSettingsFileStore(base *EncryptedStore) *SettingsFileStore { return &SettingsFileStore{base} }
func (s *SettingsFileStore) Load(ctx context.Context) (*SettingsDocument, error) {
	s.base.mu.Lock()
	defer s.base.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	f, e := os.Open(filepath.Join(s.base.dir, "settings-v1.dpapi"))
	if os.IsNotExist(e) {
		old, e := os.Open(filepath.Join(s.base.dir, "engine.json"))
		if os.IsNotExist(e) {
			return nil, nil
		}
		if e != nil {
			return nil, ErrSecrets
		}
		defer old.Close()
		raw, e := io.ReadAll(io.LimitReader(old, MaxFrame+1))
		if e != nil {
			return nil, ErrSecrets
		}
		defer clear(raw)
		settings, e := windowsengine.ParseSettings(raw)
		if e != nil {
			return nil, ErrSecrets
		}
		d := SettingsDocument{Revision: 1, Settings: settings}
		if e = s.saveLocked(ctx, d); e != nil {
			return nil, e
		}
		return &d, nil
	}
	if e != nil {
		return nil, ErrSecrets
	}
	defer f.Close()
	encrypted, e := io.ReadAll(io.LimitReader(f, MaxFrame+65537))
	if e != nil || len(encrypted) > MaxFrame+65536 {
		return nil, ErrSecrets
	}
	raw, e := Unprotect(encrypted)
	if e != nil {
		return nil, ErrSecrets
	}
	defer clear(raw)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var d SettingsDocument
	var extra any
	if decoder.Decode(&d) != nil || decoder.Decode(&extra) != io.EOF || d.Revision == 0 || d.Settings.Validate() != nil {
		return nil, ErrSecrets
	}
	return &d, nil
}
func (s *SettingsFileStore) Save(ctx context.Context, d SettingsDocument) error {
	s.base.mu.Lock()
	defer s.base.mu.Unlock()
	return s.saveLocked(ctx, d)
}
func (s *SettingsFileStore) saveLocked(ctx context.Context, d SettingsDocument) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if d.Revision == 0 || d.Settings.Validate() != nil {
		return ErrSecrets
	}
	raw, e := json.Marshal(d)
	if e != nil || len(raw) > MaxFrame {
		return ErrSecrets
	}
	defer clear(raw)
	encrypted, e := Protect(raw)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(s.base.dir, "settings-*.tmp")
	if e != nil {
		return ErrSecrets
	}
	defer os.Remove(f.Name())
	_, e = f.Write(encrypted)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return ErrSecrets
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	from, _ := windows.UTF16PtrFromString(f.Name())
	to, _ := windows.UTF16PtrFromString(filepath.Join(s.base.dir, "settings-v1.dpapi"))
	if windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH) != nil {
		return ErrSecrets
	}
	return nil
}
