package admin

import (
	"embed"
	"html/template"
	"net/http"
)

var _ embed.FS

//go:embed mdm_editor.html
var mdmEditorHTML string

//go:embed mdm_editor.js
var mdmEditorJS []byte
var mdmEditorTemplate = template.Must(template.New("mdm-editor").Parse(mdmEditorHTML))

func (w *Web) mdmEditor(rw http.ResponseWriter, r *http.Request) {
	session, ok := w.authorized(rw, r, false)
	if !ok || !w.mdmReady(rw) {
		return
	}
	state, e := w.MDM.DeviceState(r.URL.Query().Get("id"))
	if e != nil {
		http.NotFound(rw, r)
		return
	}
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = mdmEditorTemplate.Execute(rw, mdmView{Base: w.base, CSRF: session.CSRF, Device: state.Binding.ID, UserID: state.UserID, Users: w.Store.List()})
}
func (w *Web) mdmEditorScript(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	rw.Write(mdmEditorJS)
}
