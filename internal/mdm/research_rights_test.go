package mdm

import (
	"encoding/json"
	"testing"
)

func TestResearchRightsDefaultAndRevocation(t *testing.T) {
	var old Rights
	if err := json.Unmarshal([]byte(`{"telemetry":true,"geo":true}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Research {
		t.Fatal("old telemetry grant must not imply research consent")
	}
	s, b, _ := fixture(t)
	b, err := s.Activate(b.ID, testNow)
	if err != nil {
		t.Fatal(err)
	}
	granted := Rights{Research: true}
	req := SyncRequest{Version: 1, BindingID: b.ID, Epoch: b.Epoch, GrantedRights: &granted}
	if _, err = s.Sync(b.ID, req, testNow); err != nil {
		t.Fatal(err)
	}
	if !s.state.Bindings[b.ID].Binding.GrantedRights.Research {
		t.Fatal("research consent lost")
	}
	req.GrantedRights = &old
	if _, err = s.Sync(b.ID, req, testNow); err != nil {
		t.Fatal(err)
	}
	if s.state.Bindings[b.ID].Binding.GrantedRights.Research {
		t.Fatal("research consent not revoked")
	}
}
