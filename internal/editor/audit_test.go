package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuditPersistsOnlyMetadataAndExpires(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	a := NewAudit(dir, func() time.Time { return now })
	if e := a.Append(AuditEvent{Session: strings.Repeat("a", 64), Action: "created"}); e != nil {
		t.Fatal(e)
	}
	b := NewAudit(dir, func() time.Time { return now })
	events, e := b.Read("")
	if e != nil || len(events) != 1 {
		t.Fatal(events, e)
	}
	if e = b.Append(AuditEvent{Session: "SECRET-TOKEN", Action: "created"}); e == nil {
		t.Fatal("invalid metadata accepted")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "2026-10-11.jsonl"))
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "configuration") {
		t.Fatal("payload in audit")
	}
	now = now.AddDate(0, 0, 91)
	events, e = b.Read("")
	if e != nil || len(events) != 0 {
		t.Fatal(events, e)
	}
	if _, e = os.Stat(filepath.Join(dir, "2026-10-11.jsonl")); !os.IsNotExist(e) {
		t.Fatal("old audit retained")
	}
}
func TestAuditFailurePreventsNewEditorAuthority(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-directory")
	os.WriteFile(dir, []byte("x"), 0600)
	s := NewStore(nil)
	s.audit = NewAudit(dir, nil)
	if _, e := s.Create("owner", 30*time.Minute); e == nil {
		t.Fatal("unaudited authority created")
	}
	if len(s.sessions) != 0 {
		t.Fatal("session left after failed audit")
	}
}
func TestAuditLifecycleAndAckRetry(t *testing.T) {
	s := NewStore(nil)
	s.EnableAudit(t.TempDir())
	ticket, _ := s.Create("owner", 30*time.Minute)
	grant, e := s.Pair(ticket.ID, ticket.PairToken, 0, document)
	if e != nil {
		t.Fatal(e)
	}
	rev, e := s.Submit(ticket.ID, "owner", ticket.BrowserToken, 0, document)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e = s.Acknowledge(ticket.ID, grant.DeviceToken, rev, 1, document, "applied"); e != nil {
			t.Fatal(e)
		}
	}
	s.RevokeOwner("owner")
	events, e := s.AuditEvents(ticket.ID)
	if e != nil {
		t.Fatal(e)
	}
	actions := []string{}
	for _, v := range events {
		actions = append(actions, v.Action)
	}
	if strings.Join(actions, ",") != "created,paired,submitted,applied,logout" {
		t.Fatal(actions)
	}
}
func TestAuditRecoversTruncatedTail(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	a := NewAudit(dir, func() time.Time { return now })
	id := strings.Repeat("b", 64)
	if e := a.Append(AuditEvent{Session: id, Action: "created"}); e != nil {
		t.Fatal(e)
	}
	f, _ := os.OpenFile(filepath.Join(dir, "2026-10-11.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(`{"at":"unfinished`)
	f.Close()
	if e := a.Append(AuditEvent{Session: id, Action: "paired"}); e != nil {
		t.Fatal(e)
	}
	events, e := a.Read(id)
	if e != nil || len(events) != 2 {
		t.Fatal(events, e)
	}
}
func TestAuditSelectedDay(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	a := NewAudit(t.TempDir(), func() time.Time { return now })
	if e := a.Append(AuditEvent{Session: strings.Repeat("c", 64), Action: "created"}); e != nil {
		t.Fatal(e)
	}
	now = now.AddDate(0, 0, 1)
	rows, e := a.Read("")
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	rows, e = a.Read("", "2026-10-10")
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	if _, e = a.Read("", "../../secret"); e == nil {
		t.Fatal("invalid date accepted")
	}
}
