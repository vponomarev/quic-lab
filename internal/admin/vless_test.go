package admin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"quiclab/internal/vless"
	"quiclab/internal/vlessserver"
	"strings"
	"testing"
	"time"
)

func managedVLESSFixture(t *testing.T) (*Store, func()) {
	t.Helper()
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	os.Chmod(s.admissionDirectory(), 0700)
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	w, e := vlessserver.OpenWorker(context.Background(), dir, func(context.Context, string, string) (time.Duration, error) { return time.Second, nil })
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { w.Close() })
	stop, e := vlessserver.ServeControl(context.Background(), w)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(stop)
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := l.Addr().String()
	l.Close()
	c := &vlessserver.Config{Listen: addr, Endpoint: addr, Security: "reality", Mode: "standalone", ServerName: "cover.example", Fingerprint: "chrome", RealityTarget: "127.0.0.1:24443", RealityServerNames: []string{"cover.example"}, RealityPrivateKey: base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("x", 32))), RealityShortIDs: []string{"aabb"}}
	if e = s.ConfigureVLESS(c, &vlessserver.ControlClient{Dir: dir}); e != nil {
		t.Fatal(e)
	}
	return s, stop
}
func TestVLESSEnrollmentSeparateCredentials(t *testing.T) {
	s, _ := managedVLESSFixture(t)
	u, e := s.CreateWithProtocols("VLESS", []string{"vless"})
	if e != nil {
		t.Fatal(e)
	}
	_, token, e := s.NewEnrollment(u.ID, time.Hour, 5)
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.Enroll(token, "phone-one", "one")
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Enroll(token, "phone-two", "two")
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Enroll(token, "phone-one", "one")
	if e != nil {
		t.Fatal(e)
	}
	uriA, e := s.VLESSProfile(a.ID)
	if e != nil {
		t.Fatal(e)
	}
	uriB, e := s.VLESSProfile(b.ID)
	if e != nil {
		t.Fatal(e)
	}
	pa, e := vless.ParseImport(uriA)
	if e != nil {
		t.Fatal(e)
	}
	pb, e := vless.ParseImport(uriB)
	if e != nil {
		t.Fatal(e)
	}
	if pa.Config.UUID == a.ID || pa.Config.UUID == pb.Config.UUID || a.VLESSUUID != again.VLESSUUID {
		t.Fatal("credential identity invalid")
	}
	metadata, _ := json.Marshal(s.List())
	devices, _ := json.Marshal(s.Devices(u.ID))
	if strings.Contains(string(metadata)+string(devices), pa.Config.UUID) {
		t.Fatal("metadata exposes UUID")
	}
	if e = s.DisableDevice(a.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.VLESSProfile(a.ID); e == nil {
		t.Fatal("disabled export")
	}
	if _, e = s.VLESSProfile(b.ID); e != nil {
		t.Fatal("sibling export interrupted")
	}
}
func TestVLESSWorkerFailureDoesNotIssueProfile(t *testing.T) {
	s, stop := managedVLESSFixture(t)
	u, e := s.CreateWithProtocols("VLESS", []string{"vless"})
	if e != nil {
		t.Fatal(e)
	}
	stop()
	if _, e = s.VLESSProfile(u.ID); e == nil {
		t.Fatal("unconfirmed credentials exported")
	}
	if e = s.DisableDevice(u.ID); e == nil {
		t.Fatal("unconfirmed revocation reported success")
	}
	if _, e = s.Profile(u.ID); e == nil {
		t.Fatal("revocation rolled back")
	}
}
func TestVLESSReenableRotatesRevokedUUID(t *testing.T) {
	s, _ := managedVLESSFixture(t)
	u, e := s.CreateWithProtocols("VLESS", []string{"vless"})
	if e != nil {
		t.Fatal(e)
	}
	before, e := s.VLESSProfile(u.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetProtocols(u.ID, []string{"vless"}, true); e != nil {
		t.Fatal(e)
	}
	if e = s.SetProtocols(u.ID, []string{"vless"}, false); e != nil {
		t.Fatal(e)
	}
	after, e := s.VLESSProfile(u.ID)
	if e != nil || after == before {
		t.Fatal("revoked credential reused")
	}
}
func TestVLESSAdmissionRejectsOldUUIDAfterRotation(t *testing.T) {
	s, _ := managedVLESSFixture(t)
	u, e := s.CreateWithProtocols("VLESS", []string{"vless"})
	if e != nil {
		t.Fatal(e)
	}
	before, e := s.VLESSProfile(u.ID)
	if e != nil {
		t.Fatal(e)
	}
	p, _ := vless.ParseImport(before)
	stop, e := s.StartVLESSAdmission(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	s.admission.Quarantine(0)
	if _, e := vlessserver.RequestAdmission(context.Background(), s.admissionDirectory(), u.ID, p.Config.UUID); e != nil {
		t.Fatal("current credential denied")
	}
	if e := s.SetProtocols(u.ID, []string{"vless"}, true); e != nil {
		t.Fatal(e)
	}
	if e := s.SetProtocols(u.ID, []string{"vless"}, false); e != nil {
		t.Fatal(e)
	}
	if _, e := vlessserver.RequestAdmission(context.Background(), s.admissionDirectory(), u.ID, p.Config.UUID); e == nil {
		t.Fatal("old worker credential admitted")
	}
}
func TestVLESSSharesAdmissionAndRevokesDemux(t *testing.T) {
	s, _ := managedVLESSFixture(t)
	s.ConfigureDeviceLimit(1)
	u, e := s.CreateWithProtocols("mixed", []string{"quic", "https", "vless"})
	if e != nil {
		t.Fatal(e)
	}
	other, e := s.CreateDevice(u.ID, "other")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := s.VLESSProfile(u.ID)
	if e != nil {
		t.Fatal(e)
	}
	p, _ := vless.ParseImport(raw)
	stop, e := s.StartVLESSAdmission(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	s.admission.Quarantine(0)
	closed := false
	release, e := s.RegisterProtocol(deviceTLS(t, u.Certificate), "quic", func() { closed = true })
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	for range 5 {
		if _, e := vlessserver.RequestAdmission(context.Background(), s.admissionDirectory(), u.ID, p.Config.UUID); e != nil {
			t.Fatal("same device consumed extra slot")
		}
	}
	if _, e := s.RegisterProtocol(deviceTLS(t, other.Certificate), "https", func() {}); e == nil {
		t.Fatal("shared limit bypassed")
	}
	if e := s.DisableDevice(u.ID); e != nil {
		t.Fatal(e)
	}
	if !closed {
		t.Fatal("demux session survived revoke")
	}
}
func TestConfiguredAdmissionLimitDefaultsAndMaximum(t *testing.T) {
	if a := NewAdmission(0); a.limit != 30 {
		t.Fatal("default admission is not 30")
	}
	if a := NewAdmission(100); a.limit != 100 {
		t.Fatal("configured capacity above 30 was reduced")
	}
}

func TestVLESSCapacityRejectedBeforePersistence(t *testing.T) {
	s, _ := managedVLESSFixture(t)
	u, e := s.CreateWithProtocols("capacity", []string{"vless"})
	if e != nil {
		t.Fatal(e)
	}
	for i := 1; i < 512; i++ {
		id := fmt.Sprintf("retired-%d", i)
		s.state.Devices[id] = Device{ID: id, UserID: u.ID, VLESSUUID: fmt.Sprintf("%08x-1111-4111-8111-111111111111", i), Disabled: true, Expires: time.Now().Add(time.Hour)}
	}
	if e := s.save(); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(s.path)
	if _, e := s.CreateDevice(u.ID, "overflow"); e == nil {
		t.Fatal("overflow committed")
	}
	after, _ := os.ReadFile(s.path)
	if !bytes.Equal(before, after) {
		t.Fatal("overflow changed durable identities")
	}
	d := s.state.Devices[u.ID]
	d.VLESSRevoked = true
	if e := s.provisionVLESS(&d, u); e != nil {
		t.Fatal("rotation at capacity rejected")
	}
}
