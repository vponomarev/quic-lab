package mdm

import "testing"

func TestGrantedRightsAreReportedAndRevoked(t *testing.T) {
	s, b, _ := fixture(t)
	b, err := s.Activate(b.ID, testNow)
	if err != nil {
		t.Fatal(err)
	}
	granted := Rights{Config: true, Geo: true, LANMode: "deny"}
	req := SyncRequest{Version: 1, BindingID: b.ID, Epoch: b.Epoch, GrantedRights: &granted}
	if _, err = s.Sync(b.ID, req, testNow); err != nil {
		t.Fatal(err)
	}
	if s.state.Bindings[b.ID].Binding.GrantedRights != granted {
		t.Fatal("grants not recorded")
	}
	events, _ := s.Audit(b.ID)
	count := len(events)
	if _, err = s.Sync(b.ID, req, testNow); err != nil {
		t.Fatal(err)
	}
	events, _ = s.Audit(b.ID)
	if len(events) != count {
		t.Fatal("unchanged grants duplicated audit")
	}
	denied := Rights{LANMode: "deny"}
	req.GrantedRights = &denied
	if _, err = s.Sync(b.ID, req, testNow); err != nil {
		t.Fatal(err)
	}
	if s.state.Bindings[b.ID].Binding.GrantedRights != denied {
		t.Fatal("revocation not recorded")
	}
}
