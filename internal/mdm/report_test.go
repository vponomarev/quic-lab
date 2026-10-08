package mdm

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func reportFixture(t *testing.T) (*Store, Binding, DeviceReport) {
	s, b, _ := fixture(t)
	b, e := s.Activate(b.ID, testNow)
	if e != nil {
		t.Fatal(e)
	}
	rights := Rights{Config: true, VPN: true}
	if _, e = s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &rights}, testNow); e != nil {
		t.Fatal(e)
	}
	r := DeviceReport{Version: 1, Sequence: 1, ConfigGeneration: 3, Capabilities: []string{"web-control-v1"}, Name: "Phone", AppVersion: "test", VPNState: "stopped", Configuration: json.RawMessage(validDocument), Inventory: &AppInventory{Entries: []AppEntry{{PackageID: "org.test.app", Label: "Приложение"}}, Hash: "v1", Complete: false}}
	return s, b, r
}
func TestReportsRightsAndOrdering(t *testing.T) {
	s, b, r := reportFixture(t)
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal("idempotent retry", e)
	}
	r.Name = "Different"
	if e := s.Report(b.ID, b.Epoch, r, testNow); !errors.Is(e, ErrConflict) {
		t.Fatal("same sequence different data", e)
	}
	r.Sequence = 2
	r.Configuration = json.RawMessage(strings.Replace(validDocument, `"settings":{`, `"identity":{"vless_uri":"secret"},"settings":{`, 1))
	if e := s.Report(b.ID, b.Epoch, r, testNow); e == nil {
		t.Fatal("secret accepted")
	}
	r.Configuration = json.RawMessage(validDocument)
	r.Inventory.Entries = append(r.Inventory.Entries, r.Inventory.Entries[0])
	if e := s.Report(b.ID, b.Epoch, r, testNow); e == nil {
		t.Fatal("duplicate package")
	}
	r.Inventory = nil
	r.Version = 2
	if e := s.Report(b.ID, b.Epoch, r, testNow); e == nil {
		t.Fatal("version")
	}
	r.Version = 1
	rights := Rights{}
	s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &rights}, testNow)
	if e := s.Report(b.ID, b.Epoch, r, testNow); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("missing config rights", e)
	}
}
func TestDesiredCASRetry(t *testing.T) {
	s, b, r := reportFixture(t)
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	d, e := s.SetDesiredChecked(b.ID, 0, 3, "apply-1", "current", json.RawMessage(validDocument))
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.SetDesiredChecked(b.ID, 0, 3, "apply-1", "current", json.RawMessage(validDocument))
	if e != nil || again.Revision != d.Revision {
		t.Fatal("retry", e)
	}
	if _, e = s.SetDesiredChecked(b.ID, 0, 3, "apply-2", "current", json.RawMessage(validDocument)); !errors.Is(e, ErrConflict) {
		t.Fatal("revision race", e)
	}
	if _, e = s.SetDesiredChecked(b.ID, 1, 2, "apply-3", "current", json.RawMessage(validDocument)); !errors.Is(e, ErrConflict) {
		t.Fatal("generation race", e)
	}
	cmd, e := s.QueueVPNOnce(b.ID, "vpn_start", "cmd-1", testNow)
	if e != nil {
		t.Fatal(e)
	}
	retry, e := s.QueueVPNOnce(b.ID, "vpn_start", "cmd-1", testNow.Add(time.Second))
	if e != nil || cmd.ID != retry.ID {
		t.Fatal("command retry", e)
	}
	if _, e = s.QueueVPNOnce(b.ID, "vpn_stop", "cmd-1", testNow); !errors.Is(e, ErrConflict) {
		t.Fatal("request changed", e)
	}
	if cmd.ExpiresAt.Sub(cmd.IssuedAt) != 5*time.Minute {
		t.Fatal("TTL")
	}
	state, e := s.DeviceState(b.ID)
	if e != nil || state.Report == nil || state.Desired == nil {
		t.Fatal("state", e)
	}
	state.Report.Name = "mutated"
	fresh, _ := s.DeviceState(b.ID)
	if fresh.Report.Name == "mutated" {
		t.Fatal("state not copied")
	}
}
func TestReportInventoryLimits(t *testing.T) {
	for _, kind := range []string{"count", "label", "secret", "stale", "paused"} {
		t.Run(kind, func(t *testing.T) {
			s, b, r := reportFixture(t)
			switch kind {
			case "count":
				r.Inventory.Entries = make([]AppEntry, 2049)
			case "label":
				r.Inventory.Entries[0].Label = strings.Repeat("я", 257)
			case "secret":
				r.Configuration = json.RawMessage(strings.Replace(validDocument, `"settings":{`, `"settings":{"token":"private",`, 1))
			case "stale":
				if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
					t.Fatal(e)
				}
				r.Sequence = 0
			case "paused":
				s.Pause(b.ID, b.Epoch, testNow)
			}
			if e := s.Report(b.ID, b.Epoch, r, testNow); e == nil {
				t.Fatal("invalid report accepted")
			}
		})
	}
}
func TestPendingIdentitySurvivesEditorRead(t *testing.T) {
	s, b, r := reportFixture(t)
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	uri := "vless://12345678-1234-1234-1234-123456789abc@example.com:443?security=tls&type=tcp&sni=example.com"
	var doc ConfigurationDocument
	json.Unmarshal([]byte(validDocument), &doc)
	doc.Profiles[0].Identity = &ConfigurationIdentity{VLESS: uri}
	raw, _ := json.Marshal(doc)
	if _, e := s.SetDesiredChecked(b.ID, 0, 3, "key-1", "current", raw); e != nil {
		t.Fatal(e)
	}
	observed, _ := s.DeviceState(b.ID)
	if strings.Contains(string(observed.Desired.Document), uri) {
		t.Fatal("GET secret")
	}
	if _, e := s.SetDesiredChecked(b.ID, 1, 3, "key-2", "current", observed.Desired.Document); e != nil {
		t.Fatal(e)
	}
	response, e := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch}, testNow)
	if e != nil {
		t.Fatal(e)
	}
	var result ConfigurationDocument
	json.Unmarshal(response.DesiredConfig.Document, &result)
	if result.Profiles[0].Identity == nil || result.Profiles[0].Identity.VLESS != uri {
		t.Fatal("pending credentials lost")
	}
}
