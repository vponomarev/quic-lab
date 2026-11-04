package mdm

import (
	"crypto/tls"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUserConfigHTTPBoundary(t *testing.T) {
	s, b, _, req := userEditFixture(t)
	h := NewHandler(s)
	post := func(path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://mdm.test/mdm/v1/"+path, strings.NewReader(body))
		r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	read := `{"version":1,"bindingId":"` + b.ID + `","epoch":` + jsonNumber(b.Epoch) + `}`
	token := strings.Repeat("a", 64)
	if w := post("config-editor", read, token); w.Code != 200 {
		t.Fatal("editor read", w.Code)
	}
	if w := post("config-editor", read, "wrong"); w.Code != 401 {
		t.Fatal("wrong token", w.Code)
	}
	if w := post("config-editor", strings.TrimSuffix(read, "}")+`,"userConfigEdit":true}`, token); w.Code != 400 {
		t.Fatal("injected permission", w.Code)
	}
	p, _ := s.SetUserConfigEdit(b.ID, 0, true)
	req.PolicyRevision = p.Revision
	body, _ := json.Marshal(req)
	if w := post("config-submit", string(body)+` {}`, token); w.Code != 400 {
		t.Fatal("trailing JSON", w.Code)
	}
	w := post("config-submit", string(body), token)
	if w.Code != 200 || strings.Contains(w.Body.String(), "document") || strings.Contains(w.Body.String(), "profiles") {
		t.Fatal("submit response", w.Code, w.Body.String())
	}
	if w := post("config-submit", string(body), "wrong"); w.Code != 401 {
		t.Fatal("retry bypassed auth", w.Code)
	}
}

func jsonNumber(n int64) string { raw, _ := json.Marshal(n); return string(raw) }
