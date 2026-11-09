package admin

import (
	"net/url"
	"path/filepath"
	"quiclab/internal/mdm"
	"strings"
	"testing"
	"time"
)

// Keep previously deployed map capabilities in every subsequent server release.
func TestMDMMapSurvivesReleaseIntegration(t *testing.T) {
	c := config(t)
	store, err := OpenStore(c.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	web := NewWeb(c, store)
	web.MDM, err = mdm.OpenStore(filepath.Join(c.DataDir, "mdm"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	invite, err := web.MDM.CreateInvitation(now, mdm.Rights{Telemetry: true, Geo: true})
	if err != nil {
		t.Fatal(err)
	}
	device, err := web.MDM.Redeem(mdm.EnrollmentRequest{Version: 1, Token: invite.Token, RegistrationID: "release-map", Secret: strings.Repeat("b", 64)}, now)
	if err != nil {
		t.Fatal(err)
	}
	handler := web.Handler()
	login := call(handler, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	page := call(handler, "GET", "/mdm?device="+device.ID, "", cookie)
	for _, marker := range []string{`id="cell-track"`, `id="track-map"`, `id="track-start"`, `id="track-end"`, `id="mdm-refresh"`, "mdm-track.js"} {
		if !strings.Contains(page.Body.String(), marker) {
			t.Errorf("published MDM history lost %s", marker)
		}
	}
	for _, path := range []string{"/mdm-track.js", "/vendor/leaflet/leaflet.js", "/vendor/leaflet/leaflet.css"} {
		if result := call(handler, "GET", path, "", cookie); result.Code != 200 {
			t.Errorf("map asset %s: %d", path, result.Code)
		}
	}
}
