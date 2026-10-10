package admin

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"quiclab/internal/awg"
	"quiclab/internal/backup"
	"quiclab/internal/mdm"
	"quiclab/internal/transit"
	"quiclab/internal/vlessserver"
	"strings"
)

func ValidateBackup(v *backup.Verified, restoring bool) error {
	if v == nil {
		return errors.New("missing verified backup")
	}
	allowedConfig := map[string]bool{"admin.json": true, "server.json": true, "cert.pem": true, "key.pem": true, "client-ca.pem": true, "transit.json": true, "vless-cert.pem": true, "vless-key.pem": true}
	for _, entry := range v.Manifest.Entries {
		path := entry.Path
		ok := path == "data/identities.json" || path == "data/mdm/state.json" || path == "data/vless/vless-state.json"
		if strings.HasPrefix(path, "config/") {
			ok = allowedConfig[strings.TrimPrefix(path, "config/")]
		}
		if v.Manifest.Kind == backup.Full {
			ok = ok || strings.HasPrefix(path, "data/mdm/radio/") && strings.HasSuffix(path, ".json") || strings.HasPrefix(path, "data/client-diagnostics/") && (strings.HasSuffix(path, ".json") || strings.HasSuffix(path, "/version.meta")) || strings.HasPrefix(path, "data/downloads/capture/") && (strings.HasSuffix(path, ".pcapng") || strings.HasSuffix(path, ".pcap"))
		}
		if !ok {
			return errors.New("unsupported backup component")
		}
	}
	read := func(name string) ([]byte, error) {
		p := filepath.Join(v.Dir, name)
		info, e := os.Stat(p)
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() || info.Size() > 64<<20 {
			return nil, errors.New("backup component exceeds size limit")
		}
		return os.ReadFile(p)
	}
	raw, err := read("config/admin.json")
	if err != nil {
		return errors.New("missing admin configuration")
	}
	var cfg Config
	if json.Unmarshal(raw, &cfg) != nil || cfg.Validate() != nil {
		return errors.New("invalid backup admin configuration")
	}
	if cfg.Transit != nil {
		raw, err = read("config/transit.json")
		if err != nil {
			return errors.New("missing uplink credentials")
		}
		var uplink struct {
			Transit transit.Config `json:"transit"`
			AWG     string         `json:"awg_config"`
		}
		if json.Unmarshal(raw, &uplink) != nil || uplink.Transit.Validate() != nil {
			return errors.New("invalid uplink configuration")
		}
		if _, err = awg.Parse(uplink.AWG); err != nil {
			return errors.New("invalid uplink credentials")
		}
	}
	for _, names := range [][2]string{{"config/cert.pem", "config/key.pem"}, {"config/vless-cert.pem", "config/vless-key.pem"}} {
		cp, ce := read(names[0])
		kp, ke := read(names[1])
		if os.IsNotExist(ce) && os.IsNotExist(ke) {
			continue
		}
		if ce != nil || ke != nil {
			return errors.New("missing backup TLS material")
		}
		if _, err = tls.X509KeyPair(cp, kp); err != nil {
			return errors.New("invalid backup TLS key pair")
		}
	}
	if worker, e := read("data/vless/vless-state.json"); e == nil {
		if e = vlessserver.ValidateBackupState(worker); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	raw, err = read("data/identities.json")
	if err != nil {
		return err
	}
	var state diskState
	if json.Unmarshal(raw, &state) != nil || state.Version != 2 || state.Users == nil || state.Devices == nil {
		return errors.New("invalid backup identities")
	}
	ca, err := tls.X509KeyPair([]byte(state.CA), []byte(state.Key))
	if err != nil {
		return errors.New("invalid backup CA")
	}
	cert, err := x509.ParseCertificate(ca.Certificate[0])
	if err != nil || !cert.IsCA {
		return errors.New("invalid backup CA")
	}
	for id, u := range state.Users {
		if id != u.ID || u.Name == "" {
			return errors.New("invalid backup user")
		}
	}
	for id, d := range state.Devices {
		if id != d.ID || state.Users[d.UserID].ID != d.UserID {
			return errors.New("invalid backup device reference")
		}
		pair, e := tls.X509KeyPair([]byte(d.Certificate), []byte(d.Key))
		if e != nil {
			return errors.New("invalid backup device key")
		}
		leaf, e := x509.ParseCertificate(pair.Certificate[0])
		if e != nil || leaf.CheckSignatureFrom(cert) != nil {
			return errors.New("invalid backup device certificate")
		}
	}
	raw, err = read("data/mdm/state.json")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	out, err := mdm.BackupState(raw, v.Manifest.Kind, restoring)
	if err != nil {
		return err
	}
	var bindings struct {
		Bindings map[string]struct{ UserID string }
	}
	if json.Unmarshal(raw, &bindings) != nil {
		return errors.New("invalid MDM backup")
	}
	for _, b := range bindings.Bindings {
		if b.UserID != "" && state.Users[b.UserID].ID != b.UserID {
			return errors.New("MDM backup references missing user")
		}
	}
	if !restoring {
		return nil
	}
	name := filepath.Join(v.Dir, "data/mdm/state.json")
	tmp, err := os.CreateTemp(filepath.Dir(name), ".restore-state-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(out)
	ce := tmp.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	if err = os.Rename(tmp.Name(), name); err != nil {
		return err
	}
	hash := sha256.Sum256(out)
	for i := range v.Manifest.Entries {
		if v.Manifest.Entries[i].Path == "data/mdm/state.json" {
			v.Manifest.Entries[i].Size = int64(len(out))
			v.Manifest.Entries[i].SHA256 = hex.EncodeToString(hash[:])
		}
	}
	return nil
}
