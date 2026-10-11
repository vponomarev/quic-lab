package mdm

import (
	"encoding/json"
	"strings"
	"testing"
)

func scopeDoc(mode string) json.RawMessage {
	return json.RawMessage(strings.Replace(validDocument, `"schema":1`, `"researchScope":{"mode":"`+mode+`","packages":["app.test"]},"schema":1`, 1))
}
func TestResearchScopeDocument(t *testing.T) {
	for _, mode := range []string{"vpn_only", "vpn_plus_apps", "all_apps"} {
		if e := ValidateDocument(scopeDoc(mode)); e != nil {
			t.Fatal(mode, e)
		}
	}
	if ValidateDocument(scopeDoc("bad")) == nil {
		t.Fatal("invalid mode")
	}
	var old Rights
	json.Unmarshal([]byte(`{"research":true}`), &old)
	if old.ResearchDirect {
		t.Fatal("old rights expanded")
	}
}
func TestResearchScopeRightsAndCapabilities(t *testing.T) {
	s, b, r := reportFixture(t)
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SetDesiredChecked(b.ID, 0, 3, "req-old", "current", scopeDoc("all_apps")); e == nil {
		t.Fatal("old client accepted")
	}
	r.Sequence++
	r.Capabilities = append(r.Capabilities, "research_scope_v1")
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SetDesiredChecked(b.ID, 0, 3, "req-denied", "current", scopeDoc("all_apps")); e == nil {
		t.Fatal("direct grant bypass")
	}
	rights := Rights{Config: true, Research: true, ResearchDirect: true}
	if _, e := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &rights}, testNow); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SetDesiredChecked(b.ID, 0, 3, "req-allowed", "current", scopeDoc("all_apps")); e != nil {
		t.Fatal(e)
	}
}

func TestDirectAdmissionUsesReportedDeviceRights(t *testing.T) {
	s, b, r := reportFixture(t)
	if e := s.AssignUser(b.ID, "u", testNow); e != nil {
		t.Fatal(e)
	}
	r.ResearchDeviceIDs = []string{"d"}
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}
	if s.ResearchDirectAllowed("u", "d") {
		t.Fatal("managed device without direct right admitted")
	}
	if !s.ResearchDirectAllowed("u", "standalone") {
		t.Fatal("standalone wrongly blocked")
	}
	rights := Rights{Config: true, Research: true, ResearchDirect: true}
	if _, e := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &rights}, testNow); e != nil {
		t.Fatal(e)
	}
	if !s.ResearchDirectAllowed("u", "d") {
		t.Fatal("authorized managed device blocked")
	}
	rights.ResearchDirect = false
	if _, e := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &rights}, testNow); e != nil {
		t.Fatal(e)
	}
	r.Sequence++
	r.ResearchDeviceIDs = nil
	r.VPNState = ""
	if e := s.Report(b.ID, b.Epoch, r, testNow); e != nil {
		t.Fatal(e)
	}

	if s.ResearchDirectAllowed("u", "d") {
		t.Fatal("revoked managed device admitted")
	}
}
