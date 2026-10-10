package mdm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"quiclab/internal/backup"
	"testing"
)

func TestSnapshotMDMConfigScope(t *testing.T) {
	s, b, _ := fixture(t)
	v := s.state.Bindings[b.ID]
	v.Audit = []auditEntry{{Event: Event{ID: "sensitive-history"}}}
	v.Commands = []Command{{ID: "old-start", Kind: "vpn_start"}}
	v.Report = &DeviceReport{}
	v.Seen["dedupe"] = testNow
	s.state.Bindings[b.ID] = v
	if err := s.save(s.state); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := BackupState(raw, backup.Config, false)
	if err != nil {
		t.Fatal(err)
	}
	var got snapshot
	json.Unmarshal(out, &got)
	x := got.Bindings[b.ID]
	if len(x.Audit) != 0 || x.Report != nil || x.SecretHash != v.SecretHash || x.Seen["dedupe"] != testNow || len(x.Commands) != 1 {
		t.Fatal("config scope or identity changed")
	}
	out, err = BackupState(raw, backup.Full, true)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(out, &got)
	x = got.Bindings[b.ID]
	if len(x.Commands) != 0 || len(x.Audit) != 1 || x.Report == nil {
		t.Fatal("restore did not cancel only pending commands")
	}
}
