package admin

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestVLESSWebExportAndAuthorization(t *testing.T) {
	s, _ := managedVLESSFixture(t)
	c := config(t)
	c.DataDir = s.admissionDirectory()
	w := NewWeb(c, s)
	h := w.Handler()
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	csrf := w.sessions[cookie.Value].CSRF
	u, e := s.CreateWithProtocols("vless", []string{"vless"})
	if e != nil {
		t.Fatal(e)
	}
	form := url.Values{"csrf": {csrf}, "id": {u.ID}}
	exported := call(h, "POST", "/users/config", form.Encode(), cookie)
	var p Profile
	if exported.Code != 200 || json.Unmarshal(exported.Body.Bytes(), &p) != nil || p.VLESSURI == "" || p.Key != "" {
		t.Fatal("managed VLESS export missing")
	}
	qr := call(h, "POST", "/users/vless-qr", form.Encode(), cookie)
	if qr.Code != 200 || !strings.Contains(qr.Body.String(), "data:image/png;base64,") {
		t.Fatal("VLESS QR missing")
	}
	if strings.Contains(qr.Body.String(), "закрытый ключ AWG") {
		t.Fatal("VLESS QR claims to contain AWG private key")
	}
	users := call(h, "GET", "/users", "", cookie)
	addForm := strings.Split(strings.Split(users.Body.String(), "<form class=\"add-user\"")[1], "</form>")[0]
	if !strings.Contains(addForm, "value=\"vless\"") {
		t.Fatal("VLESS unavailable in new-user form")
	}
	form.Del("csrf")
	if r := call(h, "POST", "/users/vless-qr", form.Encode(), cookie); r.Code != 403 {
		t.Fatal("QR CSRF bypass")
	}
	if r := call(h, "POST", "/vless/config", form.Encode(), cookie); r.Code != 403 {
		t.Fatal("settings CSRF bypass")
	}
	if r := call(h, "GET", "/vless", "", nil); r.Code != 303 {
		t.Fatal("settings authentication bypass")
	}
	page := call(h, "GET", "/vless", "", cookie)
	if page.Code != 200 || strings.Contains(page.Body.String(), s.state.VLESS.RealityPrivateKey) {
		t.Fatal("settings leaked private key")
	}
}

func TestVLESSToolsAreDraftOnlyAndAuthenticated(t *testing.T) {
	s, _ := managedVLESSFixture(t)
	c := config(t)
	w := NewWeb(c, s)
	h := w.Handler()
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	csrf := w.sessions[cookie.Value].CSRF
	old := s.vlessConfig().RealityPrivateKey
	for _, path := range []string{"generate-key", "generate-short-id", "check-target"} {
		if r := call(h, "POST", "/vless/"+path, "", nil); r.Code != 303 {
			t.Fatal("tool missing authentication")
		}
		if r := call(h, "POST", "/vless/"+path, "", cookie); r.Code != 403 {
			t.Fatal("tool missing CSRF")
		}
	}
	r := call(h, "POST", "/vless/generate-key", url.Values{"csrf": {csrf}}.Encode(), cookie)
	var keys map[string]string
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &keys) != nil || keys["private_key"] == "" || keys["public_key"] == "" {
		t.Fatal("key generation unavailable")
	}
	if s.vlessConfig().RealityPrivateKey != old {
		t.Fatal("generation changed active key")
	}
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("generated key cacheable")
	}
	page := call(h, "GET", "/vless", "", cookie)
	if !strings.Contains(page.Body.String(), "expected_revision") || !strings.Contains(page.Body.String(), "reality_spider_x") {
		t.Fatal("settings contract absent")
	}
}

func TestVLESSSettingsPreserveKeyAndRejectStaleOrUnacknowledgedChanges(t *testing.T) {
	s, _ := managedVLESSFixture(t)
	c := config(t)
	w := NewWeb(c, s)
	h := w.Handler()
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	csrf := w.sessions[cookie.Value].CSRF
	initial := s.vlessConfig()
	_, _, rev := s.VLESSStatus()
	form := url.Values{"csrf": {csrf}, "expected_revision": {fmt.Sprint(rev)}, "listen": {initial.Listen}, "endpoint": {initial.Endpoint}, "security": {initial.Security}, "server_name": {initial.ServerName}, "fingerprint": {initial.Fingerprint}, "flow": {initial.Flow}, "mode": {initial.Mode}, "demux_endpoint": {initial.DemuxEndpoint}, "reality_target": {initial.RealityTarget}, "reality_names": {strings.Join(initial.RealityServerNames, ",")}, "reality_ids": {"ccdd"}}
	if r := call(h, "POST", "/vless/config", form.Encode(), cookie); r.Code != 303 {
		t.Fatalf("blank key save: %d %s", r.Code, r.Body.String())
	}
	if s.vlessConfig().RealityPrivateKey != initial.RealityPrivateKey {
		t.Fatal("blank key rotated active key")
	}
	if r := call(h, "POST", "/vless/config", form.Encode(), cookie); r.Code != 409 {
		t.Fatal("stale form accepted")
	}
	_, _, rev = s.VLESSStatus()
	form.Set("expected_revision", fmt.Sprint(rev))
	form.Set("reality_private_key", initial.RealityPrivateKey)
	if r := call(h, "POST", "/vless/config", form.Encode(), cookie); r.Code != 400 {
		t.Fatal("key change without acknowledgement accepted")
	}
	form.Set("confirm_key_change", "yes")
	if r := call(h, "POST", "/vless/config", form.Encode(), cookie); r.Code != 303 {
		t.Fatalf("acknowledged save: %d", r.Code)
	}
}
