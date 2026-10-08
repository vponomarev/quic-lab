package admin

import (
	"net/url"
	"path/filepath"
	"quiclab/internal/mdm"
	"strings"
	"testing"
	"time"
)

func TestMDMAdminConsentAndAccess(t *testing.T) {
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
	h := w.Handler()
	if out := call(h, "GET", "/mdm", "", nil); out.Code != 303 {
		t.Fatal("public MDM", out.Code)
	}
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	form := url.Values{"csrf": {w.sessions[cookie.Value].CSRF}}
	bad := call(h, "POST", "/mdm/invite", "csrf=wrong", cookie)
	if bad.Code != 403 {
		t.Fatal("csrf")
	}
	out := call(h, "POST", "/mdm/invite", form.Encode(), cookie)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "quiclab://mdm/enroll#") {
		t.Fatal("invitation", out.Code, out.Body.String())
	}
	inv, _ := w.MDM.CreateInvitation(time.Now(), mdm.Rights{Telemetry: true, Geo: true})
	b, e := w.MDM.Redeem(mdm.EnrollmentRequest{Version: 1, Token: inv.Token, RegistrationID: "test", Secret: strings.Repeat("a", 64)}, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	form.Set("id", b.ID)
	form.Set("enabled", "on")
	form.Set("days", "90")
	out = call(h, "POST", "/mdm/policy", form.Encode(), cookie)
	if out.Code != 303 {
		t.Fatal("policy", out.Code)
	}
	// An admin request does not grant device consent.
	if w.MDM.Bindings()[0].Binding.GrantedRights.Geo {
		t.Fatal("admin silently granted geo")
	}
	out = call(h, "GET", "/mdm?device="+b.ID, "", cookie)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "Wi-Fi") || !strings.Contains(out.Body.String(), "нет согласия") {
		t.Fatal("detail", out.Code, out.Body.String())
	}
}
