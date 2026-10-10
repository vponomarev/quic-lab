package admin

import (
	"encoding/json"
	"net/url"
	"quiclab/internal/editor"
	"testing"
)

func TestTemporaryEditorBrowserIsolation(t *testing.T) {
	c := config(t)
	s, _ := OpenStore(c.DataDir)
	w := NewWeb(c, s)
	h := w.Handler()
	if r := call(h, "POST", "/editor/create", "duration=30", nil); r.Code != 303 {
		t.Fatal("anonymous", r.Code)
	}
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	csrf := w.sessions[cookie.Value].CSRF
	form := url.Values{"csrf": {csrf}, "duration": {"30"}}
	r := call(h, "POST", "/editor/create", form.Encode(), cookie)
	if r.Code != 200 {
		t.Fatal("create", r.Code)
	}
	var ticket editor.Ticket
	if e := json.Unmarshal(r.Body.Bytes(), &ticket); e != nil || ticket.ID == "" {
		t.Fatal("ticket", e)
	}
	form.Set("id", ticket.ID)
	form.Set("token", ticket.BrowserToken)
	if r = call(h, "POST", "/editor/state", form.Encode(), cookie); r.Code != 200 {
		t.Fatal("read", r.Code)
	}
	form.Set("token", ticket.PairToken)
	if r = call(h, "POST", "/editor/state", form.Encode(), cookie); r.Code != 403 {
		t.Fatal("pair token read", r.Code)
	}
	form.Set("token", ticket.BrowserToken)
	form.Set("csrf", "bad")
	if r = call(h, "POST", "/editor/revoke", form.Encode(), cookie); r.Code != 403 {
		t.Fatal("csrf", r.Code)
	}
	second := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil).Result().Cookies()[0]
	form.Set("csrf", w.sessions[second.Value].CSRF)
	if r = call(h, "POST", "/editor/state", form.Encode(), second); r.Code != 403 {
		t.Fatal("different browser owner", r.Code)
	}
	form.Set("csrf", csrf)
	if r = call(h, "POST", "/editor/revoke", form.Encode(), cookie); r.Code != 200 {
		t.Fatal("revoke", r.Code)
	}
	if r = call(h, "POST", "/editor/state", form.Encode(), cookie); r.Code != 403 {
		t.Fatal("revoked", r.Code)
	}
}

func TestEditorPortableTransferDoesNotPublish(t *testing.T) {
	c := config(t)
	s, _ := OpenStore(c.DataDir)
	w := NewWeb(c, s)
	h := w.Handler()
	cookie := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil).Result().Cookies()[0]
	document := `{"schema":1,"profiles":[{"id":"default","name":"Test","settings":{}}],"currentProfileId":"default","enabledProfileIds":[],"multiple":false,"globalApps":[],"dns":{"mode":"system","profileId":"default"},"budget":{"limitBytes":0},"diagnostics":{"enabled":true,"detailed":false}}`
	f := url.Values{"csrf": {w.sessions[cookie.Value].CSRF}, "action": {"export"}, "document": {document}, "selection": {`{"sections":["diagnostics"]}`}}
	r := call(h, "POST", "/editor/transfer", f.Encode(), cookie)
	if r.Code != 200 {
		t.Fatal("export", r.Code)
	}
	var out struct {
		Bundle json.RawMessage `json:"bundle"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &out)
	f.Set("action", "import")
	f.Set("bundle", string(out.Bundle))
	f.Set("options", `{"mode":"update","mapping":{}}`)
	if r = call(h, "POST", "/editor/transfer", f.Encode(), cookie); r.Code != 200 {
		t.Fatal("preview", r.Code)
	}
	f.Set("bundle", `{"format":"quic-lab-config","version":1,"credentials":"included","payload":{}}`)
	if r = call(h, "POST", "/editor/transfer", f.Encode(), cookie); r.Code != 400 {
		t.Fatal("plain credentials", r.Code)
	}
}
