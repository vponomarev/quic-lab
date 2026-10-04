package admin

import (
	_ "embed"
	"net/http"
)

//go:embed ui.js
var uiJS []byte

func (w *Web) uiScript(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	rw.Write(uiJS)
}

//go:embed web.html
var pageHTML string

//go:embed ui.css
var uiCSS []byte

func (w *Web) uiStyle(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/css; charset=utf-8")
	rw.Write(uiCSS)
}

//go:embed vless_settings.js
var vlessSettingsJS []byte

func (w *Web) vlessSettingsScript(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	rw.Write(vlessSettingsJS)
}
