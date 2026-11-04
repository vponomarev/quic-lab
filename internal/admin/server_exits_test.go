package admin

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"quiclab/internal/awgserver"
	"quiclab/internal/serverexit"
	"strings"
	"testing"
)

const exitAWG = "[Interface]\nPrivateKey=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=\nAddress=10.55.0.2/32\nDNS=192.0.2.2\n[Peer]\nPublicKey=AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=\nEndpoint=198.18.0.2:51820\nAllowedIPs=0.0.0.0/0\n"

func exitDefinitions() []serverexit.Definition {
	return []serverexit.Definition{{ID: "west", Name: "West", Protocol: "awg", AWGConfig: exitAWG}, {ID: "east", Name: "East", Protocol: "awg", AWGConfig: strings.ReplaceAll(exitAWG, "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=", "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")}}
}
func grantExits(t *testing.T, s *Store, user string, p *serverexit.Policy) {
	t.Helper()
	view, err := s.UserServerExitPolicy(user)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetServerExitPolicy(user, p, view.Revision); err != nil {
		t.Fatal(err)
	}
}
func TestServerExitDeviceSelectionAndRevocation(t *testing.T) {
	s, _, d, token := updateFixture(t)
	if err := s.ConfigureServerExits(exitDefinitions()); err != nil {
		t.Fatal(err)
	}
	s.SetServerExitApply(func([]string) error { return nil })
	legacy, err := s.DeviceServerExits(d.ID, token)
	if err != nil || !legacy.Legacy {
		t.Fatal(legacy, err)
	}
	if _, err := s.SelectServerExit(d.ID, token, "west", legacy.Revision); err == nil {
		t.Fatal("self-granted exit")
	}
	p := &serverexit.Policy{Allowed: []string{"west", "east"}, Default: "west"}
	grantExits(t, s, d.UserID, p)
	p.Allowed[0] = "tampered"
	v, err := s.DeviceServerExits(d.ID, token)
	if err != nil || v.Effective != "west" || len(v.Choices) != 2 {
		t.Fatal(v, err)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "PrivateKey") || strings.Contains(string(raw), token) {
		t.Fatal("secret response")
	}
	if _, err = s.SelectServerExit(d.ID, "", "east", v.Revision); !errors.Is(err, ErrUpdateUnauthorized) {
		t.Fatal("unauthenticated selection", err)
	}
	chosen, err := s.SelectServerExit(d.ID, token, "east", v.Revision)
	if err != nil || chosen.Effective != "east" {
		t.Fatal(chosen, err)
	}
	if _, err = s.SelectServerExit(d.ID, token, "west", v.Revision); !errors.Is(err, ErrServerExitRevision) {
		t.Fatal("stale choice", err)
	}
	sibling, err := s.CreateDevice(d.UserID, "sibling")
	if err != nil {
		t.Fatal(err)
	}
	if id, err := s.ServerExitForDevice(sibling.ID); err != nil || id != "west" {
		t.Fatal("selection affected sibling", id, err)
	}
	reopened, err := OpenStore(filepath.Dir(s.path))
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.ConfigureServerExits(exitDefinitions()); err != nil {
		t.Fatal(err)
	}
	if id, err := reopened.ServerExitForDevice(d.ID); err != nil || id != "east" {
		t.Fatal("selection not durable", id, err)
	}
	grantExits(t, s, d.UserID, &serverexit.Policy{Allowed: []string{"west"}, Default: "west"})
	if _, err := s.ServerExitForDevice(d.ID); err == nil {
		t.Fatal("revoked choice fell back")
	}
	v, err = s.DeviceServerExits(d.ID, token)
	if err != nil || !v.Blocked || v.Selected != "east" {
		t.Fatal(v, err)
	}
	if _, err = s.SelectServerExit(d.ID, token, "", v.Revision); err != nil {
		t.Fatal("cannot explicitly return to default", err)
	}
	if id, err := s.ServerExitForDevice(d.ID); err != nil || id != "west" {
		t.Fatal(id, err)
	}
}

func TestServerExitApplyFailureAndSaveRollback(t *testing.T) {
	s, _, d, token := updateFixture(t)
	s.ConfigureServerExits(exitDefinitions())
	s.SetServerExitApply(func([]string) error { return nil })
	grantExits(t, s, d.UserID, &serverexit.Policy{Allowed: []string{"west", "east"}, Default: "west"})
	v, _ := s.DeviceServerExits(d.ID, token)
	s.syncDir = func(string) error { return errors.New("disk unavailable") }
	if _, err := s.SelectServerExit(d.ID, token, "east", v.Revision); err == nil {
		t.Fatal("save failure hidden")
	}
	if id, err := s.ServerExitForDevice(d.ID); err != nil || id != "west" {
		t.Fatal("unpublished choice used", id, err)
	}
	s.syncDir = nil
	s.SetServerExitApply(func([]string) error { return errors.New("worker unavailable") })
	if _, err := s.SelectServerExit(d.ID, token, "east", v.Revision); !errors.Is(err, ErrServerExitApply) {
		t.Fatal("runtime failure acknowledged", err)
	}
	if id, err := s.ServerExitForDevice(d.ID); err != nil || id != "east" {
		t.Fatal("persisted desired selection lost", id, err)
	}
	if err := s.DisableDevice(d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ServerExitForDevice(d.ID); err == nil {
		t.Fatal("disabled device routed")
	}
}

func TestServerExitBrokerUsesAuthenticatedIngressIdentity(t *testing.T) {
	s, _, d, _ := updateFixture(t)
	s.ConfigureServerExits(exitDefinitions())
	s.SetServerExitApply(func([]string) error { return nil })
	s.mu.Lock()
	u := s.state.Users[d.UserID]
	u.Protocols = []string{"quic", "vless", "awg"}
	s.state.Users[u.ID] = u
	d.VLESSUUID = "11111111-1111-4111-8111-111111111111"
	d.AWG = &awgserver.Peer{Address: "10.11.0.2"}
	s.state.Devices[d.ID] = d
	s.mu.Unlock()
	grantExits(t, s, d.UserID, &serverexit.Policy{Allowed: []string{"west", "east"}, Default: "west"})
	for _, request := range [][2]string{{"vless", d.ID}, {"awg", d.VLESSUUID}, {"https", d.ID}} {
		if _, _, _, release, err := s.AcquireServerExit(request[0], request[1], func() {}); err == nil {
			release()
			t.Fatal("forged protocol identity", request)
		}
	}
	closed := false
	id, exit, _, release, err := s.AcquireServerExit("vless", d.VLESSUUID, func() { closed = true })
	if err != nil || id != d.ID || exit != "west" {
		t.Fatal(id, exit, err)
	}
	defer release()
	grantExits(t, s, d.UserID, &serverexit.Policy{Allowed: []string{}})
	if !closed {
		t.Fatal("old exit flow remained open")
	}
	if _, _, _, _, err := s.AcquireServerExit("awg", d.ID, func() {}); err == nil {
		t.Fatal("revoked policy admitted")
	}
}
