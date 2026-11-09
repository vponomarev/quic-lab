package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"quiclab/internal/mdm"
	"strings"
	"testing"
	"time"
)

func TestCellResolverValidationAndCache(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("cellid") != "197359621" || len(r.URL.Query()) != 6 {
			t.Error("unexpected provider payload", r.URL.RawQuery)
		}
		w.Write([]byte(`{"result":200,"data":{"lat":"55.8","lon":37.6,"range":"16000"}}`))
	}))
	defer srv.Close()
	g := newCellResolver()
	g.endpoint = srv.URL
	g.interval = 0
	cell := mdm.CellRadio{Technology: "LTE", MCC: "250", MNC: "99", TAC: "1397", CI: "197359621"}
	for i := 0; i < 2; i++ {
		p, e := g.resolve(context.Background(), cell)
		if e != nil || p == nil || p.Lat != 55.8 || p.Range != 16000 {
			t.Fatalf("%+v %v", p, e)
		}
	}
	if calls != 1 {
		t.Fatal("cache missed", calls)
	}
	cell.Technology = "NR"
	if _, e := g.resolve(context.Background(), cell); e == nil {
		t.Fatal("NR silently treated as LTE")
	}
}
func TestCellResolverRejectsIncompleteAndTransientResponses(t *testing.T) {
	for _, body := range []string{`{"result":200,"data":{"lon":37,"range":4}}`, `{"result":200,"data":{"lat":91,"lon":37,"range":4}}`, `{"result":503}`, `{"result":200,"data":{"lat":0,"lon":0,"range":-1}}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		g := newCellResolver()
		g.endpoint = srv.URL
		g.interval = 0
		_, e := g.resolve(context.Background(), mdm.CellRadio{Technology: "LTE", MCC: "250", MNC: "99", TAC: "1", CI: "2"})
		srv.Close()
		if e == nil {
			t.Fatal("accepted", body)
		}
		if len(g.cache) != 0 {
			t.Fatal("cached failure")
		}
	}
}
func TestTrackEndpointsRequireLogin(t *testing.T) {
	c := config(t)
	s, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	w := NewWeb(c, s)
	for _, path := range []string{"/mdm/track", "/mdm/cell-location"} {
		out := call(w.Handler(), "POST", path, "", nil)
		if out.Code != 303 {
			t.Fatal(path, out.Code)
		}
	}
}

func TestTrackAssetsAndPrivacy(t *testing.T) {
	c := config(t)
	c.ManagementOnly = true
	s, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	w := NewWeb(c, s)
	w.MDM, e = mdm.OpenStore(filepath.Join(c.DataDir, "mdm"))
	if e != nil {
		t.Fatal(e)
	}
	h := w.Handler()
	for _, path := range []string{"/mdm-track.js", "/vendor/leaflet/leaflet.js", "/vendor/leaflet/leaflet.css"} {
		if out := call(h, "GET", path, "", nil); out.Code != 200 {
			t.Errorf("asset %s: %d", path, out.Code)
		}
	}
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	now := time.Now().UTC()
	inv, _ := w.MDM.CreateInvitation(now, mdm.Rights{Telemetry: true, Geo: true})
	b, e := w.MDM.Redeem(mdm.EnrollmentRequest{Version: 1, Token: inv.Token, RegistrationID: "track", Secret: strings.Repeat("a", 64)}, now)
	if e != nil {
		t.Fatal(e)
	}
	b, e = w.MDM.Activate(b.ID, now)
	if e != nil {
		t.Fatal(e)
	}
	_, e = w.MDM.Sync(b.ID, mdm.SyncRequest{Version: 1, Epoch: b.Epoch, GrantedRights: &mdm.Rights{Telemetry: true, Geo: true}}, now)
	if e != nil {
		t.Fatal(e)
	}
	e = w.MDM.AppendTelemetry(mdm.TelemetryBatch{Version: 1, BindingID: b.ID, Epoch: b.Epoch, Records: []mdm.RadioSample{{ID: "track-test", MeasuredAt: now, DeviceName: "private-name", WiFi: &mdm.WiFiRadio{SSID: "private-ssid"}, Cells: []mdm.CellRadio{{Technology: "LTE", Registered: true, MCC: "250", MNC: "99", TAC: "42", CI: "123"}}}}}, now)
	if e != nil {
		t.Fatal(e)
	}
	page := call(h, "GET", "/mdm?device="+b.ID, "", cookie)
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "https://tile.openstreetmap.org") {
		t.Error("CSP blocks tiles")
	}
	if !strings.Contains(page.Body.String(), `id="cell-track"`) {
		t.Error("missing map")
	}
	form := url.Values{"csrf": {w.sessions[cookie.Value].CSRF}, "id": {b.ID}, "start": {now.Add(-time.Hour).Format(time.RFC3339Nano)}, "end": {now.Add(time.Hour).Format(time.RFC3339Nano)}}
	providerCalls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { providerCalls++; rw.Write([]byte(`{"result":404}`)) }))
	defer provider.Close()
	w.cellGeo.endpoint = provider.URL
	t.Cleanup(func() {
		if w.cellDB != nil {
			w.cellDB.Close()
		}
	})
	out := call(h, "POST", "/mdm/track", form.Encode(), cookie)
	var rows []map[string]any
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &rows) != nil || len(rows) != 1 {
		t.Fatal(out.Code, out.Body.String())
	}
	for i := 0; i < 2; i++ {
		lookup := url.Values{"csrf": {w.sessions[cookie.Value].CSRF}, "technology": {"LTE"}, "mcc": {"250"}, "mnc": {"99"}, "tac": {"42"}, "ci": {"123"}}
		response := call(h, "POST", "/mdm/cell-location", lookup.Encode(), cookie)
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if providerCalls != 0 {
		t.Fatal("page lookup called external provider", providerCalls)
	}
	if strings.Contains(out.Body.String(), "private-") {
		t.Error("unnecessary private fields")
	}
	for _, path := range []string{"/mdm/track", "/mdm/cell-location"} {
		if out := call(h, "POST", path, "csrf=wrong", cookie); out.Code != 403 {
			t.Error("csrf", path, out.Code)
		}
	}
}
func TestCellResolverNegativeCacheAndCancellation(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.Write([]byte(`{"result":404}`)) }))
	defer srv.Close()
	g := newCellResolver()
	g.endpoint = srv.URL
	g.interval = 0
	c := mdm.CellRadio{Technology: "LTE", MCC: "250", MNC: "99", TAC: "42", CI: "123"}
	for i := 0; i < 2; i++ {
		p, e := g.resolve(context.Background(), c)
		if p != nil || e != nil {
			t.Fatal(p, e)
		}
	}
	if calls != 1 {
		t.Fatal("negative cache", calls)
	}
	g.gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := g.resolve(ctx, c); e == nil {
		t.Fatal("cancelled queue accepted")
	}
	<-g.gate
}
