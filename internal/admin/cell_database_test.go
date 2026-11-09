package admin

import (
	"context"
	"os"
	"path/filepath"
	"quiclab/internal/mdm"
	"strings"
	"testing"
	"time"
)

func TestDiscoveryFailureDoesNotRequeueCompletedCells(t *testing.T) {
	c := config(t)
	s, _ := OpenStore(c.DataDir)
	w := NewWeb(c, s)
	w.MDM, _ = mdm.OpenStore(filepath.Join(c.DataDir, "mdm"))
	db, e := w.geoStore()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	now := time.Now().UTC()
	inv, _ := w.MDM.CreateInvitation(now, mdm.Rights{Telemetry: true, Geo: true})
	b, e := w.MDM.Redeem(mdm.EnrollmentRequest{Version: 1, Token: inv.Token, RegistrationID: "discovery", Secret: strings.Repeat("a", 64)}, now)
	if e != nil {
		t.Fatal(e)
	}
	b, _ = w.MDM.Activate(b.ID, now)
	w.MDM.Sync(b.ID, mdm.SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &mdm.Rights{Telemetry: true, Geo: true}}, now)
	e = w.MDM.AppendTelemetry(mdm.TelemetryBatch{Version: 1, BindingID: b.ID, Epoch: b.Epoch, Records: []mdm.RadioSample{{ID: "cell", MeasuredAt: now.Add(-time.Minute), Cells: []mdm.CellRadio{{Technology: "LTE", Registered: true, MCC: "250", MNC: "99", TAC: "1", CI: "1"}}}}}, now)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(c.DataDir, "mdm/radio", b.ID, now.Add(-72*time.Hour).Format("2006-01-02")+".json"), []byte("broken"), 0600); e != nil {
		t.Fatal(e)
	}
	db.SetMeta("backfill", cellBackfill{ID: b.ID, Start: now.Add(-time.Hour), End: now, State: "pending"})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if e = w.discoverCells(cancelled, db); e == nil {
		t.Fatal("cancelled discovery ignored")
	}
	var interrupted cellBackfill
	db.Meta("backfill", &interrupted)
	if interrupted.State == "failed" {
		t.Fatal("shutdown made discovery permanently failed")
	}
	if e = w.discoverCells(context.Background(), db); e == nil {
		t.Fatal("expected corrupt history failure")
	}
	j, e := db.Next(now)
	if e != nil || j == nil {
		t.Fatal(j, e)
	}
	if e = db.Complete(*j, nil, nil, now); e != nil {
		t.Fatal(e)
	}
	if e = w.discoverCells(context.Background(), db); e != nil {
		t.Fatal("failed discovery restarted", e)
	}
	stats, _ := db.Stats()
	if stats.Pending != 0 {
		t.Fatal("completed cell requeued", stats)
	}
	var job cellBackfill
	db.Meta("backfill", &job)
	if job.State != "failed" {
		t.Fatal(job)
	}
}
