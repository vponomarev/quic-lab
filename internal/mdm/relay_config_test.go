package mdm

import (
	"encoding/json"
	"errors"
	"testing"
)

func relayDocument(t *testing.T) ConfigurationDocument {
	t.Helper()
	var d ConfigurationDocument
	json.Unmarshal([]byte(validDocument), &d)
	d.Profiles[0].Settings["https_endpoint"] = json.RawMessage(`"gateway.example:443"`)
	d.Profiles = append(d.Profiles, ConfigurationProfile{ID: "11111111-1111-4111-8111-111111111111", Name: "Relay", Settings: map[string]json.RawMessage{"transport": json.RawMessage(`"vless"`), "relay_exit_profile_id": json.RawMessage(`"default"`)}})
	return d
}
func TestRelayConfigurationReferences(t *testing.T) {
	d := relayDocument(t)
	raw, _ := json.Marshal(d)
	if e := ValidateDocument(raw); e != nil {
		t.Fatal("valid reference rejected", e)
	}
	for _, tc := range []string{"missing", "self", "target-vless", "target-no-https", "remove-identity", "source-quic", "deleted-target", "cycle"} {
		t.Run(tc, func(t *testing.T) {
			d := relayDocument(t)
			switch tc {
			case "missing":
				d.Profiles[1].Settings["relay_exit_profile_id"] = json.RawMessage(`"22222222-2222-4222-8222-222222222222"`)
			case "self":
				d.Profiles[1].Settings["relay_exit_profile_id"] = json.RawMessage(`"11111111-1111-4111-8111-111111111111"`)
			case "target-vless":
				d.Profiles[0].Settings["transport"] = json.RawMessage(`"vless"`)
			case "target-no-https":
				delete(d.Profiles[0].Settings, "https_endpoint")
			case "remove-identity":
				d.Profiles[0].RemoveIdentity = true
			case "deleted-target":
				d.Profiles = d.Profiles[1:]
			case "cycle":
				d.Profiles[0].Settings["relay_exit_profile_id"] = json.RawMessage(`"11111111-1111-4111-8111-111111111111"`)
			case "source-quic":
				d.Profiles[1].Settings["transport"] = json.RawMessage(`"quic"`)
			}
			raw, _ := json.Marshal(d)
			if ValidateDocument(raw) == nil {
				t.Fatal("invalid reference accepted")
			}
		})
	}
}

func TestRelayCapabilityAndDowngrade(t *testing.T) {
	s, b, r := reportFixture(t)
	if err := s.Report(b.ID, b.Epoch, r, testNow); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(relayDocument(t))
	if _, err := s.SetDesired(b.ID, 0, "current", raw); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("old client accepted relay", err)
	}
	if _, err := s.SetDesiredChecked(b.ID, 0, 3, "old-relay", "current", raw); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("old checked client accepted relay", err)
	}
	r.Sequence++
	r.Capabilities = append(r.Capabilities, "vless_relay_v1")
	if err := s.Report(b.ID, b.Epoch, r, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDesiredChecked(b.ID, 0, 3, "new-relay", "current", raw); err != nil {
		t.Fatal(err)
	}
	req := SyncRequest{Version: 1, Epoch: b.Epoch}
	got, err := s.Sync(b.ID, req, testNow)
	if err != nil || got.DesiredConfig == nil {
		t.Fatal("capable client did not receive relay", err)
	}
	r.Sequence++
	r.Capabilities = []string{"web-control-v1"}
	if err := s.Report(b.ID, b.Epoch, r, testNow); err != nil {
		t.Fatal(err)
	}
	got, err = s.Sync(b.ID, req, testNow)
	if err != nil || got.DesiredConfig != nil {
		t.Fatal("downgraded client received relay", err)
	}
}

func TestRelayStatusBoundsAndRights(t *testing.T) {
	s, b, r := reportFixture(t)
	r.RelayStatus = &RelayStatus{ExitID: "default", Generation: 1, State: "ready", Paths: []RelayPathStatus{{Index: 1, Network: "wifi", Ready: true, Primary: true}, {Index: 2, Network: "wifi", Ready: true}}}
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	r.Sequence++
	r.RelayStatus.Paths[1].Primary = true
	if e := s.Report(b.ID, b.Epoch, r, testNow); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	r.RelayStatus.Paths[1].Primary = false
	rights := Rights{Config: true}
	if _, e := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &rights}, testNow); e != nil {
		t.Fatal(e)
	}
	r.VPNState = ""
	if e := s.Report(b.ID, b.Epoch, r, testNow); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
}

func TestRelayPendingIncompatibleIdentityRejected(t *testing.T) {
	s, b, r := reportFixture(t)
	r.Capabilities = append(r.Capabilities, "vless_relay_v1")
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	d := relayDocument(t)
	delete(d.Profiles[1].Settings, "relay_exit_profile_id")
	d.Profiles[0].Identity = &ConfigurationIdentity{VLESS: "vless://11111111-1111-4111-8111-111111111111@example.org:443?security=tls&type=tcp&sni=example.org"}
	raw, _ := json.Marshal(d)
	if _, e := s.SetDesiredChecked(b.ID, 0, 3, "stage-incompatible", "current", raw); e != nil {
		t.Fatal(e)
	}
	observed, e := s.DeviceState(b.ID)
	if e != nil {
		t.Fatal(e)
	}
	var sanitized ConfigurationDocument
	json.Unmarshal(observed.Desired.Document, &sanitized)
	if sanitized.Profiles[0].Identity != nil {
		t.Fatal("public snapshot contains private identity")
	}
	sanitized.Profiles[1].Settings["relay_exit_profile_id"] = json.RawMessage(`"default"`)
	raw, _ = json.Marshal(sanitized)
	if _, e = s.SetDesiredChecked(b.ID, 1, 3, "relay-incompatible", "current", raw); !errors.Is(e, ErrInvalid) {
		t.Fatal("pending incompatible identity accepted", e)
	}
	after, _ := s.DeviceState(b.ID)
	if after.Desired.Revision != 1 {
		t.Fatal("failed update changed desired revision")
	}
}
