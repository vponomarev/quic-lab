package admin

import (
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
