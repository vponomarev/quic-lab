package admin

import (
	"bytes"
	"encoding/json"
	"net/url"
	"quiclab/internal/research"
	"strings"
	"testing"
)

func TestResearchDetectedProtocolFilter(t *testing.T) {
	for _, name := range []string{"DNS", "HTTP", "TLS", "QUIC v1", "QUIC v2"} {
		q, err := researchQuery(url.Values{"detected_protocol": {strings.ToLower(name)}})
		if err != nil || q.DetectedProtocol != name {
			t.Fatalf("%+v %v", q, err)
		}
	}
	if _, err := researchQuery(url.Values{"detected_protocol": {"HTTP/3"}}); err == nil {
		t.Fatal("invalid protocol accepted")
	}
	rows := []researchAppStats{{ApplicationStats: research.ApplicationStats{App: research.App{Attribution: "unknown"}}}}
	groupResearchApplications(rows, url.Values{"detected_protocol": {"QUIC v1"}}, "/lab/")
	if !strings.Contains(rows[0].GroupURL, "detected_protocol=QUIC+v1") {
		t.Fatal(rows[0].GroupURL)
	}
}

func TestResearchDetectedProtocolEscapingAndOfferedALPN(t *testing.T) {
	var d research.FlowData
	if err := json.Unmarshal([]byte(`{"protocol":"udp","detected_protocol":{"name":"QUIC v1","evidence":"quic_initial"},"tls":{"state":"observed","server_name":"<script>bad</script>","alpn_offered":["h3","<img src=x>"]}}`), &d); err != nil {
		t.Fatal(err)
	}
	v := researchView{Query: research.Query{UserID: "u"}, Rows: []researchRow{{Flow: d}}}
	var b bytes.Buffer
	if err := researchTemplate.Execute(&b, v); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	if strings.Contains(html, "<script>bad") || strings.Contains(html, "<img src=x>") {
		t.Fatal("unescaped metadata")
	}
	start := strings.Index(html, "QUIC v1</strong>")
	end := strings.Index(html, "<details class=\"flow-details\">")
	if start < 0 || end < start || !strings.Contains(html[start:end], "Предложенные ALPN") || !strings.Contains(html[start:end], "h3") {
		t.Fatal("protocol and offered ALPN missing from main row")
	}
	if strings.Contains(html, "HTTP/3 пока не разбирается") {
		t.Fatal("obsolete caveat")
	}
}
