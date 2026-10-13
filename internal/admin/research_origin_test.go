package admin

import (
	"bytes"
"regexp"
"os"
	"quiclab/internal/research"
	"strings"
	"testing"
)

func TestResearchOriginRendering(t *testing.T) {
	for origin, label := range map[string]string{"client": "Источник: клиент", "server_awg": "Источник: сервер AWG", "": "Источник неизвестен (старый формат)"} {
		var out bytes.Buffer
		v := researchView{Query: research.Query{UserID: "u"}, Rows: []researchRow{{Record: research.Record{Origin: origin}, Application: "<script>"}}}
		if e := researchTemplate.Execute(&out, v); e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(out.String(), label) || strings.Contains(out.String(), "<script>") {
			t.Fatal("origin missing or application not escaped")
		}
	}
}

func TestResearchPageUsesExternalScript(t *testing.T) {
 var out bytes.Buffer
 if err := researchTemplate.Execute(&out,researchView{Query:research.Query{UserID:"fixture"}}); err != nil {t.Fatal(err)}
 page:=out.String()
 if regexp.MustCompile(`<script[^>]*>\s*[^<\s]`).MatchString(page) || !strings.Contains(page,`src="research-ui.js"`) {t.Fatal("research must work with script-src self")}
 if strings.Index(page,`id="quick-periods"`)>strings.Index(page,`<h2>Приложения</h2>`) || strings.Contains(page,`hidden id="quick-periods"`) {t.Fatal("periods must be visible before applications")}
 if path:=os.Getenv("RESEARCH_UI_FIXTURE");path!="" {if err:=os.WriteFile(path,out.Bytes(),0600);err!=nil{t.Fatal(err)}}
}
