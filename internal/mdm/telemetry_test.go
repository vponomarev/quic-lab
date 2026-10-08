package mdm

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func radioFixture(t *testing.T) (*Store, Binding, string) {
	t.Helper()
	s, b, secret := fixture(t)
	b, e := s.Activate(b.ID, testNow)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &Rights{Telemetry: true, Geo: true}}, testNow)
	if e != nil {
		t.Fatal(e)
	}
	return s, b, secret
}
func sample(at time.Time) RadioSample {
	dbm := -98
	return RadioSample{ID: "sample-1", MeasuredAt: at, DeviceName: "Xiaomi test", AppVersion: "0.9.1",
		WiFi:  &WiFiRadio{SSID: "test", BSSID: "02:11:22:33:44:55", DBM: &dbm},
		Cells: []CellRadio{{Technology: "LTE", MCC: "250", MNC: "99", CI: "12345", TAC: "42", PCI: "10", DBM: &dbm}},
	}
}
func TestTelemetryDurableIdempotentAndConsent(t *testing.T) {
	s, b, _ := radioFixture(t)
	batch := TelemetryBatch{Version: 1, BindingID: b.ID, Epoch: b.Epoch, Records: []RadioSample{sample(testNow)}}
	if e := s.AppendTelemetry(batch, testNow); e != nil {
		t.Fatal(e)
	}
	if e := s.AppendTelemetry(batch, testNow.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	again, e := OpenStore(s.dir)
	if e != nil {
		t.Fatal(e)
	}
	rows, e := again.Telemetry(b.ID, testNow, 100)
	if e != nil || len(rows) != 1 || rows[0].Sample.Cells[0].CI != "12345" || !rows[0].ReceivedAt.Equal(testNow) {
		t.Fatalf("durable dedup: %#v %v", rows, e)
	}
	_, e = s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &Rights{}}, testNow)
	if e != nil {
		t.Fatal(e)
	}
	batch.Records[0].ID = "sample-2"
	if e = s.AppendTelemetry(batch, testNow); e == nil {
		t.Fatal("accepted without consent")
	}
	rows, e = s.Telemetry(b.ID, testNow, 100)
	if e != nil || len(rows) != 1 {
		t.Fatal("revocation removed historical data", e)
	}
}
func TestTelemetryRejectsStaleEpochPausedAndMalformed(t *testing.T) {
	s, b, _ := radioFixture(t)
	batch := TelemetryBatch{Version: 1, BindingID: b.ID, Epoch: b.Epoch, Records: []RadioSample{sample(testNow)}}
	batch.Epoch--
	if e := s.AppendTelemetry(batch, testNow); e == nil {
		t.Fatal("stale epoch")
	}
	batch.Epoch = b.Epoch
	batch.Records[0].MeasuredAt = testNow.Add(-25 * time.Hour)
	if e := s.AppendTelemetry(batch, testNow); e == nil {
		t.Fatal("overage")
	}
	batch.Records[0] = sample(testNow)
	batch.Records[0].Cells[0].CI = "../../bad"
	if e := s.AppendTelemetry(batch, testNow); e == nil {
		t.Fatal("bad cell identifier")
	}
	batch.Records[0] = sample(testNow)
	if e := s.Pause(b.ID, b.Epoch, testNow); e != nil {
		t.Fatal(e)
	}
	if e := s.AppendTelemetry(batch, testNow); e == nil {
		t.Fatal("paused")
	}
}
func TestTelemetryRetentionAndDelete(t *testing.T) {
	s, b, _ := radioFixture(t)
	batch := TelemetryBatch{Version: 1, BindingID: b.ID, Epoch: b.Epoch, Records: []RadioSample{sample(testNow)}}
	if e := s.AppendTelemetry(batch, testNow); e != nil {
		t.Fatal(e)
	}
	rows, e := s.Telemetry(b.ID, testNow.Add(91*24*time.Hour), 100)
	if e != nil || len(rows) != 0 {
		t.Fatal("expired history", e)
	}
	batch.Records[0] = sample(testNow.Add(91 * 24 * time.Hour))
	if e = s.AppendTelemetry(batch, testNow.Add(91*24*time.Hour)); e != nil {
		t.Fatal(e)
	}
	if e = s.DeleteTelemetry(b.ID); e != nil {
		t.Fatal(e)
	}
	rows, e = s.Telemetry(b.ID, testNow.Add(91*24*time.Hour), 100)
	if e != nil || len(rows) != 0 {
		t.Fatal("delete failed", e)
	}
}
func TestTelemetryHTTPRequiresRealTLSAndSecret(t *testing.T) {
	s, b, secret := radioFixture(t)
	// HTTP handler uses wall clock, unlike deterministic storage tests.
	batch := TelemetryBatch{Version: 1, BindingID: b.ID, Epoch: b.Epoch, Records: []RadioSample{sample(time.Now().UTC())}}
	raw, _ := json.Marshal(batch)
	for _, tt := range []struct {
		tls    bool
		secret string
		status int
	}{{false, secret, 426}, {true, "wrong", 401}, {true, secret, 200}} {
		r := httptest.NewRequest("POST", "https://mdm.test/mdm/v1/telemetry", bytes.NewReader(raw))
		if tt.tls {
			r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		} else {
			r.TLS = nil
		}
		r.Header.Set("Authorization", "Bearer "+tt.secret)
		w := httptest.NewRecorder()
		NewHandler(s).ServeHTTP(w, r)
		if w.Code != tt.status {
			t.Fatalf("status %d want %d body %s", w.Code, tt.status, w.Body.String())
		}
	}
	raw = bytes.Replace(raw, []byte(`"records":`), []byte(`"gps":"unexpected","records":`), 1)
	r := httptest.NewRequest("POST", "https://mdm.test/mdm/v1/telemetry", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()
	NewHandler(s).ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("unknown data accepted", w.Code)
	}
}
func TestTelemetryPolicyVisibleAndValidated(t *testing.T) {
	s, b, _ := radioFixture(t)
	p := TelemetryPolicy{Enabled: true, RetentionDays: 30}
	if e := s.SetTelemetryPolicy(b.ID, p); e != nil {
		t.Fatal(e)
	}
	out, e := s.Sync(b.ID, SyncRequest{Version: 1, Epoch: b.Epoch}, testNow)
	if e != nil || !out.Telemetry.Enabled || out.Telemetry.RetentionDays != 30 {
		t.Fatal("policy not delivered", e)
	}
	p.RetentionDays = 0
	if e = s.SetTelemetryPolicy(b.ID, p); e == nil {
		t.Fatal("invalid retention")
	}
	if _, e = s.Telemetry("../bad", testNow, 100); e == nil {
		t.Fatal("path traversal")
	}
	if strings.Contains(b.ID, "/") {
		t.Fatal("fixture")
	}
}

func TestTelemetryMaintenancePhysicallyRemovesExpiredInactiveHistory(t *testing.T) {
	s, b, _ := radioFixture(t)
	if e := s.AppendTelemetry(TelemetryBatch{Version: 1, BindingID: b.ID, Epoch: b.Epoch, Records: []RadioSample{sample(testNow)}}, testNow); e != nil {
		t.Fatal(e)
	}
	if e := s.PruneTelemetry(testNow.Add(90*24*time.Hour + time.Second)); e != nil {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(filepath.Join(s.dir, "radio", b.ID))
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatal("expired sensitive data still on disk")
	}
}
func TestTelemetryDroppedWithoutPendingRecords(t *testing.T) {
	s, b, _ := radioFixture(t)
	if e := s.AppendTelemetry(TelemetryBatch{Version: 1, BindingID: b.ID, Epoch: b.Epoch, Dropped: 7}, testNow); e != nil {
		t.Fatal(e)
	}
	if s.Bindings()[0].Dropped != 7 {
		t.Fatal("discard counter lost")
	}
	again, e := OpenStore(s.dir)
	if e != nil || again.Bindings()[0].Dropped != 7 {
		t.Fatal("counter not durable", e)
	}
}
