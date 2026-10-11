package admin

import (
	"bytes"
	"quiclab/internal/research"
	"strings"
	"testing"
)

func TestResearchOriginRendering(t *testing.T) {
	for origin, label := range map[string]string{"client": "Источник: клиент", "server_awg": "Источник: сервер AWG", "": "Источник неизвестен (старый формат)"} {
		var out bytes.Buffer
		v := researchView{Rows: []researchRow{{Record: research.Record{Origin: origin}, Application: "<script>"}}}
		if e := researchTemplate.Execute(&out, v); e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(out.String(), label) || strings.Contains(out.String(), "<script>") {
			t.Fatal("origin missing or application not escaped")
		}
	}
}
