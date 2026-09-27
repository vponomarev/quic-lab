package admin

import (
	_ "embed"
	"html/template"
	"net/http"
)

//go:embed capture-settings.html
var captureSettingsHTML string
var captureSettingsTemplate = template.Must(template.New("capture-settings").Parse(captureSettingsHTML))

//go:embed capture-settings.js
var captureSettingsJS []byte

func (w *Web) captureSettings(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, false); !ok {
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	captureSettingsTemplate.Execute(rw, struct{ Base string }{w.base})
}
func (w *Web) captureSettingsScript(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	rw.Write(captureSettingsJS)
}
