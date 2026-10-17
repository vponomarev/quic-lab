package admin

import (
	_ "embed"
	"html/template"
	"net/http"
	"quiclab/internal/mdm"
	"time"
)

func (w *Web) mdmProbe(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok || !w.mdmReady(rw) {
		return
	}
	out, e := w.MDM.QueueProbe(r.Form.Get("id"), r.Form.Get("requestID"), mdm.ProbeRequest{SourceID: r.Form.Get("sourceId"), Fingerprint: r.Form.Get("fingerprint"), Action: r.Form.Get("action"), RunID: r.Form.Get("runId")}, time.Now().UTC())
	mdmReply(rw, out, e)
}

//go:embed mdm_probe.html
var mdmProbeHTML string

//go:embed mdm_probe.js
var mdmProbeJS []byte
var mdmProbeTemplate = template.Must(template.New("mdm-probe").Parse(mdmProbeHTML))

func (w *Web) mdmProbes(rw http.ResponseWriter, r *http.Request) {
	s, ok := w.authorized(rw, r, false)
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
	mdmProbeTemplate.Execute(rw, mdmView{Base: w.base, CSRF: s.CSRF, Device: state.Binding.ID})
}
func (w *Web) mdmProbeScript(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	rw.Write(mdmProbeJS)
}
