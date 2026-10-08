package admin

import (
	"net/url"
	"path/filepath"
	"quiclab/internal/mdm"
	"strings"
	"testing"
	"time"
)

func TestMDMControlRoutesGuarded(t *testing.T) {
	c := config(t)
	s, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	w := NewWeb(c, s)
	w.MDM, e = mdm.OpenStore(filepath.Join(c.DataDir, "mdm"))
	if e != nil {
		t.Fatal(e)
	}
	inv, _ := w.MDM.CreateInvitation(time.Now(), mdm.Rights{Config: true, VPN: true})
	b, e := w.MDM.Redeem(mdm.EnrollmentRequest{Version: 1, Token: inv.Token, RegistrationID: "test", Secret: strings.Repeat("c", 64)}, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	h := w.Handler()
	if out := call(h, "GET", "/mdm/state?id="+b.ID, "", nil); out.Code != 303 {
		t.Fatal("public state")
	}
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	form := url.Values{"csrf": {w.sessions[cookie.Value].CSRF}, "id": {b.ID}, "requestID": {"one"}, "kind": {"vpn_start"}}
	out := call(h, "POST", "/mdm/command", form.Encode(), cookie)
	if out.Code != 409 {
		t.Fatal("paused command", out.Code)
	}
	b, _ = w.MDM.Activate(b.ID, time.Now())
	out = call(h, "POST", "/mdm/command", form.Encode(), cookie)
	if out.Code != 403 {
		t.Fatal("unsupported command", out.Code)
	}
	out = call(h, "GET", "/mdm/state?id="+b.ID, "", cookie)
	if out.Code != 200 || strings.Contains(out.Body.String(), strings.Repeat("c", 64)) {
		t.Fatal("state leaks secret", out.Code)
	}
	form.Set("csrf", "wrong")
	if out = call(h, "POST", "/mdm/config", form.Encode(), cookie); out.Code != 403 {
		t.Fatal("csrf", out.Code)
	}
}
func TestMDMEditorPage(t *testing.T) {
	c := config(t)
	s, _ := OpenStore(c.DataDir)
	w := NewWeb(c, s)
	w.MDM, _ = mdm.OpenStore(filepath.Join(c.DataDir, "mdm"))
	inv, _ := w.MDM.CreateInvitation(time.Now(), mdm.Rights{})
	b, _ := w.MDM.Redeem(mdm.EnrollmentRequest{Version: 1, Token: inv.Token, RegistrationID: "edit", Secret: strings.Repeat("b", 64)}, time.Now())
	h := w.Handler()
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	out := call(h, "GET", "/mdm/device?id="+b.ID, "", cookie)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "Применить") || !strings.Contains(out.Body.String(), "mdm-editor.js") {
		t.Fatal("editor missing", out.Code)
	}
	if out = call(h, "GET", "/mdm/device?id=missing", "", cookie); out.Code != 404 {
		t.Fatal("unknown device", out.Code)
	}
}

func TestMDMListUsesReportWithoutGeo(t *testing.T) {
	c := config(t)
	s, _ := OpenStore(c.DataDir)
	w := NewWeb(c, s)
	w.MDM, _ = mdm.OpenStore(filepath.Join(c.DataDir, "mdm"))
	inv, _ := w.MDM.CreateInvitation(time.Now(), mdm.Rights{})
	b, _ := w.MDM.Redeem(mdm.EnrollmentRequest{Version: 1, Token: inv.Token, RegistrationID: "report", Secret: strings.Repeat("c", 64)}, time.Now())
	b, _ = w.MDM.Activate(b.ID, time.Now())
	if e := w.MDM.Report(b.ID, b.Epoch, mdm.DeviceReport{Version: 1, Sequence: 1, Name: "Report-only phone", AppVersion: "version-123"}, time.Now()); e != nil {
		t.Fatal(e)
	}
	h := w.Handler()
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	out := call(h, "GET", "/mdm", "", cookie)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "Report-only phone") || !strings.Contains(out.Body.String(), "version-123") {
		t.Fatal("device report missing from list", out.Code)
	}
}
