package admin

import (
	"net/url"
	"testing"
)

func TestResearchScopeQuery(t *testing.T) {
	q, e := researchQuery(url.Values{"route": {"direct"}, "exit": {"direct"}})
	if e != nil || q.Route != "direct" || q.ExitID != "direct" {
		t.Fatal(q, e)
	}
	if _, e = researchQuery(url.Values{"route": {"invalid"}}); e == nil {
		t.Fatal("invalid route accepted")
	}
}
