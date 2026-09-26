package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

type sessionFile struct {
	Credential string
	Sessions   map[string]session
}

func (w *Web) credentialID() string {
	sum := sha256.Sum256([]byte(w.Config.Username + "\x00" + w.Config.Password))
	return hex.EncodeToString(sum[:])
}
func (w *Web) loadSessions() {
	if w.Store == nil {
		return
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(w.Store.path), "admin-sessions.json"))
	if err != nil {
		return
	}
	var f sessionFile
	if json.Unmarshal(b, &f) == nil && f.Credential == w.credentialID() && f.Sessions != nil {
		w.sessions = f.Sessions
		w.prune()
	}
}

// Caller holds w.mu. Atomic replacement keeps logout durable across restarts.
func (w *Web) saveSessions() error {
	b, err := json.Marshal(sessionFile{w.credentialID(), w.sessions})
	if err != nil {
		return err
	}
	dir := filepath.Dir(w.Store.path)
	f, err := os.CreateTemp(dir, ".admin-sessions-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "admin-sessions.json"))
}
