package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBondHTTPSRequiresActualTLSIdentity(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("GET", "https://example.test/tunnel/bond", nil)
	r.Header.Set("X-Client-Cert", "untrusted-header")
	w := httptest.NewRecorder()
	s.ServeBondHTTPS(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated join: %d", w.Code)
	}
}
