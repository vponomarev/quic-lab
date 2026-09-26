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
