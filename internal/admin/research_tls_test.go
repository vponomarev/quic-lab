package admin

import (
	"bytes"
	"net/url"
	"quiclab/internal/netobserve"
	"quiclab/internal/research"
	"strings"
	"testing"
)

func TestResearchECHLabelAndFilter(t *testing.T) {
	q, e := researchQuery(url.Values{"sni": {"<ECH>"}})
	if e != nil || q.SNI != "<ECH>" {
		t.Fatal(q, e)
	}
	v := researchView{Query: research.Query{UserID: "u"}, Rows: []researchRow{{Record: research.Record{Type: "connection"}, Flow: research.FlowData{TLS: &netobserve.TLSInfo{State: "observed", ServerName: "outer.example", ECHOffered: true}}}}}
	var b bytes.Buffer
	if e := researchTemplate.Execute(&b, v); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(b.String(), "&lt;ECH&gt;") || !strings.Contains(b.String(), "GREASE") {
		t.Fatal("missing truthful ECH marker")
	}
}
