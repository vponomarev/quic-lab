package admin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"quiclab/internal/backup"
	"quiclab/internal/mdm"
	"strings"
)

type backupParticipant struct {
	files          backup.FileParticipant
	config         Config
	sources        map[string]string
	prepared       map[string][]byte
	hashes         map[string][32]byte
	currentSources map[string]string
	validateServer []func([]byte) error
}

// NewBackupParticipant resolves credentials from the authoritative durable
// configuration on each snapshot; archive contents cannot select source paths.
func NewBackupParticipant(cfg Config, sources map[string]string, validateServer ...func([]byte) error) backup.Participant {
	p := &backupParticipant{config: cfg, sources: sources, validateServer: validateServer}
	p.files = backup.FileParticipant{Root: cfg.DataDir, Prefix: "data", Required: []string{"identities.json"}, Include: func(rel string, kind backup.Kind) bool {
		if rel == "identities.json" || rel == "mdm/state.json" || rel == "vless/vless-state.json" {
			return true
		}
		if kind != backup.Full {
			return false
		}
		return strings.HasPrefix(rel, "client-diagnostics/") && (strings.HasSuffix(rel, ".json") || strings.HasSuffix(rel, "/version.meta")) || strings.HasPrefix(rel, "mdm/radio/") && strings.HasSuffix(rel, ".json") || strings.HasPrefix(rel, "downloads/capture/") && (strings.HasSuffix(rel, ".pcapng") || strings.HasSuffix(rel, ".pcap"))
	}}
	return p
}
func (p *backupParticipant) Prepare(ctx context.Context, kind backup.Kind) error {
	if err := p.files.Prepare(ctx, kind); err != nil {
		return err
	}
	p.prepared = map[string][]byte{}
	p.hashes = map[string][32]byte{}
	p.currentSources = map[string]string{}
	for name, path := range p.sources {
		p.currentSources[name] = path
	}
	current := p.config
	identities, err := os.ReadFile(filepath.Join(p.config.DataDir, "identities.json"))
	if err != nil {
		return err
	}
	var state diskState
	if json.Unmarshal(identities, &state) != nil {
		return errors.New("invalid durable identities")
	}
	if state.VLESSPending != nil {
		return errors.New("VLESS configuration update in progress; retry backup")
	}
	if state.VLESS != nil {
		current.VLESS = state.VLESS
	}
	delete(p.currentSources, "config/vless-cert.pem")
	delete(p.currentSources, "config/vless-key.pem")
	if current.VLESS != nil && current.VLESS.Security == "tls" {
		cert, key := current.VLESS.TLSCertificateFile, current.VLESS.TLSKeyFile
		if cert == "/run/credentials/quic-lab-vless.service/cert.pem" && key == "/run/credentials/quic-lab-vless.service/key.pem" {
			cert, key = p.sources["config/cert.pem"], p.sources["config/key.pem"]
		}
		p.currentSources["config/vless-cert.pem"], p.currentSources["config/vless-key.pem"] = cert, key
	}
	raw, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	p.prepared["config/admin.json"] = raw
	// The AWG worker startup file is a projection of the loaded admin settings;
	// permanent private keys remain in identities.json, never in this projection.
	if current.AWG != nil {
		raw, e := json.Marshal(map[string]any{"data_dir": current.DataDir, "awg": current.AWG})
		if e != nil {
			return e
		}
		p.prepared["config/awg.json"] = raw
	}
	for name, src := range p.currentSources {
		if !strings.HasPrefix(name, "config/") || strings.Contains(name, "..") || strings.ContainsAny(name, "\\\x00") {
			return errors.New("invalid backup inventory name")
		}
		info, e := os.Stat(src)
		if e != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
			return errors.New("required backup credential unavailable")
		}
		raw, e := os.ReadFile(src)
		if e != nil || len(raw) > 4<<20 {
			return errors.New("required backup credential unavailable")
		}
		p.prepared[name] = raw
		p.hashes[name] = sha256.Sum256(raw)
	}
	return nil
}
func (p *backupParticipant) Pin(ctx context.Context, kind backup.Kind, dir string) (func() error, error) {
	if _, err := p.files.Pin(ctx, kind, dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0700); err != nil {
		return nil, err
	}
	for name, raw := range p.prepared {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
func (p *backupParticipant) Transform(ctx context.Context, kind backup.Kind, dir string) (result error) {
	defer func() {
		if result == nil && len(p.validateServer) > 0 {
			result = ValidateBackup(&backup.Verified{Dir: dir, Manifest: backup.Manifest{Kind: kind}}, false, p.validateServer...)
		}
	}()
	for name, src := range p.currentSources {
		info, err := os.Stat(src)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
			return errors.New("external credential unavailable")
		}
		raw, err := os.ReadFile(src)
		if err != nil || sha256.Sum256(raw) != p.hashes[name] {
			return errors.New("external configuration changed while creating backup")
		}
	}
	name := filepath.Join(dir, "data/mdm/state.json")
	raw, err := os.ReadFile(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	raw, err = mdm.BackupState(raw, kind, false)
	if err != nil {
		return err
	}
	// Never modify a hardlinked snapshot inode: it still belongs to live state.
	tmp, err := os.CreateTemp(filepath.Dir(name), ".backup-state-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(raw)
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), name)
}
