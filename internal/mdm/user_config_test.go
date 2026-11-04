package mdm

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func userEditFixture(t *testing.T) (*Store, Binding, DeviceReport, UserConfigRequest) {
	t.Helper()
	s, b, r := reportFixture(t)
	if err := s.Report(b.ID, b.Epoch, r, testNow); err != nil {
		t.Fatal(err)
	}
	return s, b, r, UserConfigRequest{Version: 1, BindingID: b.ID, Epoch: b.Epoch, ExpectedGeneration: r.ConfigGeneration, RequestID: "phone-save", Document: json.RawMessage(validDocument)}
}

func TestUserConfigPermissionAndProvisioning(t *testing.T) {
	s, b, r, req := userEditFixture(t)
	if _, err := s.SubmitUserConfig(req); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("self-grant", err)
	}
	policy, err := s.SetUserConfigEdit(b.ID, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	req.PolicyRevision = policy.Revision
	req.Document = json.RawMessage(strings.Replace(validDocument, `"limitBytes":0`, `"limitBytes":1048576`, 1))
	chosen, err := s.SubmitUserConfig(req)
	if err != nil || chosen.Revision != 1 {
		t.Fatal(chosen, err)
	}
	again, err := s.SubmitUserConfig(req)
	if err != nil || again.Revision != chosen.Revision {
		t.Fatal("lost response retry", again, err)
	}
	req.RequestID = "second"
	if _, err = s.SubmitUserConfig(req); !errors.Is(err, ErrConflict) {
		t.Fatal("pending revision overwritten", err)
	}
	view, err := s.UserConfig(b.ID, b.Epoch)
	if err != nil || view.Editable {
		t.Fatal("pending edit enabled", err)
	}
	out, err := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch}, testNow)
	if err != nil || out.DesiredConfig == nil || out.DesiredConfig.Revision != chosen.Revision {
		t.Fatal("not provisioned", err)
	}
	r.Sequence++
	r.AppliedRevision = chosen.Revision
	r.ConfigGeneration++
	r.Configuration = chosen.Document
	if err = s.Report(b.ID, b.Epoch, r, testNow); err != nil {
		t.Fatal(err)
	}
	view, err = s.UserConfig(b.ID, b.Epoch)
	if err != nil || !view.Editable {
		t.Fatal("applied revision not editable", err)
	}
	reopened, err := OpenStore(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	view, err = reopened.UserConfig(b.ID, b.Epoch)
	if err != nil || !view.UserConfigEdit.Enabled || view.Desired.Revision != 1 {
		t.Fatal("not durable", err)
	}
	count := 0
	for _, event := range view.Events {
		if event.Kind == "config_update" && event.Actor == "user" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate or incorrect audit", count)
	}
	req.RequestID = "phone-save"
	if got, err := reopened.SubmitUserConfig(req); err != nil || got.Revision != chosen.Revision {
		t.Fatal("retry after applied report and restart", err)
	}
}

func TestUserConfigViewOmitsDuplicateAndUnrelatedData(t *testing.T) {
	s, b, _, req := userEditFixture(t)
	p, _ := s.SetUserConfigEdit(b.ID, 0, true)
	req.PolicyRevision = p.Revision
	if _, err := s.SubmitUserConfig(req); err != nil {
		t.Fatal(err)
	}
	v, err := s.UserConfig(b.ID, b.Epoch)
	if err != nil || len(v.Report.Configuration) != 0 || v.Desired == nil {
		t.Fatal("duplicate configuration in phone view", err)
	}
	admin, _ := s.DeviceState(b.ID)
	if len(admin.Report.Configuration) == 0 {
		t.Fatal("phone projection mutated admin state")
	}
}

func TestUserConfigStalePermissionAndAdminConflict(t *testing.T) {
	s, b, _, req := userEditFixture(t)
	p, _ := s.SetUserConfigEdit(b.ID, 0, true)
	req.PolicyRevision = p.Revision
	if _, err := s.SetUserConfigEdit(b.ID, 0, false); !errors.Is(err, ErrConflict) {
		t.Fatal("stale policy accepted", err)
	}
	p, _ = s.SetUserConfigEdit(b.ID, p.Revision, false)
	if _, err := s.SubmitUserConfig(req); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked", err)
	}
	p, _ = s.SetUserConfigEdit(b.ID, p.Revision, true)
	if _, err := s.SubmitUserConfig(req); !errors.Is(err, ErrConflict) {
		t.Fatal("revoked/regranted draft", err)
	}
	req.PolicyRevision = p.Revision
	if _, err := s.SetDesiredChecked(b.ID, 0, req.ExpectedGeneration, "admin-edit", "external", json.RawMessage(validDocument)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitUserConfig(req); !errors.Is(err, ErrConflict) {
		t.Fatal("admin change overwritten", err)
	}
	req.Epoch--
	if _, err := s.SubmitUserConfig(req); !errors.Is(err, ErrConflict) {
		t.Fatal("old epoch", err)
	}
}

func TestUserConfigCannotChangeAdministrativePolicy(t *testing.T) {
	s, b, r, req := userEditFixture(t)
	var d map[string]any
	json.Unmarshal(req.Document, &d)
	d["profileSelection"] = map[string]any{"enabled": true, "profileIds": []string{"default"}, "serverRevision": 1}
	req.Document, _ = json.Marshal(d)
	p, _ := s.SetUserConfigEdit(b.ID, 0, true)
	req.PolicyRevision = p.Revision
	if _, err := s.SubmitUserConfig(req); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("self-granted selection", err)
	}
	req.Document = json.RawMessage(validDocument)
	// Revisions use the administrator's selected persistence mode.
	desired, err := s.SetDesiredChecked(b.ID, 0, r.ConfigGeneration, "admin", "external", req.Document)
	if err != nil {
		t.Fatal(err)
	}
	r.Sequence++
	r.AppliedRevision = desired.Revision
	r.ConfigGeneration++
	if err = s.Report(b.ID, b.Epoch, r, testNow); err != nil {
		t.Fatal(err)
	}
	req.ExpectedRevision = desired.Revision
	req.ExpectedGeneration = r.ConfigGeneration
	got, err := s.SubmitUserConfig(req)
	if err != nil || got.Mode != "external" {
		t.Fatal("changed persistence mode", got.Mode, err)
	}
}

func TestUserConfigFailureBoundaries(t *testing.T) {
	s, b, _, req := userEditFixture(t)
	p, _ := s.SetUserConfigEdit(b.ID, 0, true)
	req.PolicyRevision = p.Revision
	for _, raw := range []string{strings.Replace(validDocument, `"schema":1`, `"schema":1,"schema":1`, 1), strings.Replace(validDocument, `"multiple":false`, `"multiple":null`, 1), strings.Replace(validDocument, `"schema":1`, `"schema":1,"userConfigEdit":true`, 1)} {
		bad := req
		bad.Document = json.RawMessage(raw)
		if _, err := s.SubmitUserConfig(bad); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid document accepted", err)
		}
	}
	stale := req
	stale.ExpectedGeneration++
	if _, err := s.SubmitUserConfig(stale); !errors.Is(err, ErrConflict) {
		t.Fatal("stale generation", err)
	}
	original := s.dir
	s.dir = filepath.Join(original, "missing", "child")
	if _, err := s.SubmitUserConfig(req); err == nil {
		t.Fatal("write failure ignored")
	}
	s.dir = original
	if got, err := s.SubmitUserConfig(req); err != nil || got.Revision != 1 {
		t.Fatal("failure published revision", err)
	}
	if err := s.Pause(b.ID, b.Epoch, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitUserConfig(req); err == nil {
		t.Fatal("paused retry accepted")
	}
	if _, err := s.UserConfig(b.ID, b.Epoch); err == nil {
		t.Fatal("paused read accepted")
	}
}

func TestUserConfigCredentialReadIsWriteOnly(t *testing.T) {
	s, b, _, req := userEditFixture(t)
	p, _ := s.SetUserConfigEdit(b.ID, 0, true)
	req.PolicyRevision = p.Revision
	uri := "vless://11111111-1111-4111-8111-111111111111@example.org:443?security=tls&type=tcp&sni=example.org"
	req.Document = json.RawMessage(strings.Replace(validDocument, `"settings":`, `"identity":{"vless_uri":"`+uri+`"},"settings":`, 1))
	if _, err := s.SubmitUserConfig(req); err != nil {
		t.Fatal(err)
	}
	v, err := s.UserConfig(b.ID, b.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "11111111-") || strings.Contains(string(raw), "vless_uri") {
		t.Fatal("credential leaked in editor view")
	}
	out, err := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch}, testNow)
	if err != nil || out.DesiredConfig == nil || !strings.Contains(string(out.DesiredConfig.Document), "11111111-") {
		t.Fatal("credential not provisioned", err)
	}
}

func TestUserConfigConsumedCredentialRemovalDoesNotBlockReplacement(t *testing.T) {
	s, b, r, req := userEditFixture(t)
	raw := json.RawMessage(strings.Replace(validDocument, `"settings":`, `"removeIdentity":true,"settings":`, 1))
	desired, err := s.SetDesiredChecked(b.ID, 0, r.ConfigGeneration, "remove", "current", raw)
	if err != nil {
		t.Fatal(err)
	}
	r.Sequence++
	r.ConfigGeneration++
	r.AppliedRevision = desired.Revision
	if err = s.Report(b.ID, b.Epoch, r, testNow); err != nil {
		t.Fatal(err)
	}
	p, _ := s.SetUserConfigEdit(b.ID, 0, true)
	v, _ := s.UserConfig(b.ID, b.Epoch)
	if strings.Contains(string(v.Desired.Document), `"removeIdentity":true`) {
		t.Fatal("consumed removal retained in new draft")
	}
	req.PolicyRevision = p.Revision
	req.ExpectedRevision = desired.Revision
	req.ExpectedGeneration = r.ConfigGeneration
	uri := "vless://11111111-1111-4111-8111-111111111111@example.org:443?security=tls&type=tcp&sni=example.org"
	req.Document = json.RawMessage(strings.Replace(string(v.Desired.Document), `"settings":`, `"identity":{"vless_uri":"`+uri+`"},"settings":`, 1))
	if _, err = s.SubmitUserConfig(req); err != nil {
		t.Fatal("replacement after removal", err)
	}
}
