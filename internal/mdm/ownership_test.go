package mdm

import (
	"strings"
	"testing"
	"time"
)

func TestOwnershipEnrollmentAndMigration(t *testing.T) {
	s, b, _ := fixture(t)
	reopened, e := OpenStore(s.dir)
	if e != nil {
		t.Fatal(e)
	}
	if reopened.Bindings()[0].UserID != "" {
		t.Fatal("legacy owner")
	}
	inv, e := s.CreateUserInvitation(testNow, Rights{Config: true}, "user-1")
	if e != nil {
		t.Fatal(e)
	}
	req := EnrollmentRequest{Version: 1, Token: inv.Token, RegistrationID: "owned", Secret: strings.Repeat("b", 64)}
	owned, e := s.Redeem(req, testNow)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Redeem(req, testNow)
	if e != nil || again.ID != owned.ID {
		t.Fatal("retry", e)
	}
	reopened, e = OpenStore(s.dir)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, row := range reopened.Bindings() {
		if row.Binding.ID == owned.ID {
			found = true
			if row.UserID != "user-1" {
				t.Fatal("lost owner")
			}
		}
	}
	if !found || len(reopened.Bindings()) != 2 {
		t.Fatal("lost/duplicate binding")
	}
	if e = s.AssignUser(b.ID, "user-2", testNow); e != nil {
		t.Fatal(e)
	}
	if e = s.AssignUser("missing", "user-2", testNow); e == nil {
		t.Fatal("missing binding assigned")
	}
}
func TestUserDeletionKeepsMDM(t *testing.T) {
	s, b, _ := fixture(t)
	if e := s.AssignUser(b.ID, "owner", testNow); e != nil {
		t.Fatal(e)
	}
	inv, e := s.CreateUserInvitation(testNow, Rights{}, "owner")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.UnassignUser("owner", testNow.Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	rows := s.Bindings()
	if len(rows) != 1 || rows[0].UserID != "" || rows[0].Binding.ID != b.ID {
		t.Fatal("binding destroyed")
	}
	req := EnrollmentRequest{Version: 1, Token: inv.Token, RegistrationID: "after-delete", Secret: strings.Repeat("b", 64)}
	if _, e = s.Redeem(req, testNow.Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	for _, row := range s.Bindings() {
		if row.UserID != "" {
			t.Fatal("pending invite resurrected owner")
		}
	}
}
