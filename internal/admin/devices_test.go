package admin

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func deviceTLS(t *testing.T, cert string) tls.ConnectionState {
	t.Helper()
	b, _ := pem.Decode([]byte(cert))
	leaf, e := x509.ParseCertificate(b.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	return tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}, VerifiedChains: [][]*x509.Certificate{{leaf}}}
}
func TestLegacyIdentityPreserved(t *testing.T) {
	dir := t.TempDir()
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.CreateWithProtocols("legacy", []string{"quic"})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(dir, "identities.json"))
	if e != nil {
		t.Fatal(e)
	}
	var old map[string]json.RawMessage
	json.Unmarshal(raw, &old)
	old["version"] = json.RawMessage("1")
	delete(old, "devices")
	raw, _ = json.Marshal(old)
	if e = os.WriteFile(filepath.Join(dir, "identities.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	reopened, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	got, e := reopened.Profile(u.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Certificate != u.Certificate || got.Key != u.Key || reopened.state.CA != s.state.CA || reopened.state.Key != s.state.Key {
		t.Fatal("legacy credentials changed")
	}
	if e = reopened.Verify(deviceTLS(t, u.Certificate)); e != nil {
		t.Fatal(e)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "identities.json"))
	var state struct {
		Devices map[string]struct {
			ID, Certificate, Key string
			UserID               string `json:"user_id"`
			Legacy               bool
		}
	}
	json.Unmarshal(raw, &state)
	d, ok := state.Devices[u.ID]
	if !ok || len(state.Devices) != 1 || d.UserID != u.ID || !d.Legacy || d.Certificate != u.Certificate || d.Key != u.Key {
		t.Fatal("missing persistent legacy device")
	}
}

// Removing per-device ownership, save rollback, or callback unlocking breaks
// this test even when the sibling uses the same user's protocol policy.
func TestDeviceMigrationAndRevocation(t *testing.T) {
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureAWG(awgConfig()); e != nil {
		t.Fatal(e)
	}
	u, e := s.CreateWithProtocols("owner", []string{"quic", "https", "awg"})
	if e != nil {
		t.Fatal(e)
	}
	api, ok := any(s).(interface {
		CreateDevice(string, string) (Device, error)
		DisableDevice(string) error
		DeviceForTLS(tls.ConnectionState) (Device, error)
		Devices(string) []Device
	})
	if !ok {
		t.Fatal("per-device provisioning and revocation contract missing")
	}
	sibling, e := api.CreateDevice(u.ID, "second phone")
	if e != nil {
		t.Fatal(e)
	}
	if sibling.AWG == nil || sibling.AWG.Address == u.AWG.Address || sibling.AWG.Public == u.AWG.Public {
		t.Fatal("devices share AWG peer")
	}
	closed := 0
	for _, protocol := range []string{"quic", "https", "quic"} {
		_, e = s.RegisterProtocol(deviceTLS(t, u.Certificate), protocol, func() { closed++; api.Devices(u.ID) })
		if e != nil {
			t.Fatal(e)
		}
	}
	siblingClosed := 0
	release, e := s.RegisterProtocol(deviceTLS(t, sibling.Certificate), "quic", func() { siblingClosed++ })
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	oldPath := s.path
	s.path = filepath.Join(filepath.Dir(oldPath), "missing", "identities.json")
	if api.DisableDevice(u.ID) == nil {
		t.Fatal("failed save accepted")
	}
	s.path = oldPath
	if closed != 0 || s.Verify(deviceTLS(t, u.Certificate)) != nil {
		t.Fatal("save failure revoked in-memory identity")
	}
	reloads := 0
	s.awgReload = func() error { reloads++; api.Devices(u.ID); return nil }
	if e = api.DisableDevice(u.ID); e != nil {
		t.Fatal(e)
	}
	if closed != 3 || siblingClosed != 0 || reloads != 1 {
		t.Fatalf("wrong revocation effects: closed=%d sibling=%d reloads=%d", closed, siblingClosed, reloads)
	}
	if _, e = s.RegisterProtocol(deviceTLS(t, u.Certificate), "quic", func() {}); e == nil {
		t.Fatal("revoked join admitted")
	}
	if _, e = api.DeviceForTLS(deviceTLS(t, sibling.Certificate)); e != nil {
		t.Fatal("sibling authorization lost", e)
	}
	if _, e = s.AWGProfile(u.ID); e == nil {
		t.Fatal("revoked legacy AWG alias exported")
	}
	reopened, e := OpenStore(filepath.Dir(oldPath))
	if e != nil {
		t.Fatal(e)
	}
	if reopened.Verify(deviceTLS(t, u.Certificate)) == nil || reopened.Verify(deviceTLS(t, sibling.Certificate)) != nil {
		t.Fatal("device revocation not persisted")
	}
	if _, e = os.Stat(oldPath + ".bak"); e != nil {
		t.Fatal("identity backup missing", e)
	}
}
