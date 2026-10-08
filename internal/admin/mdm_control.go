package admin

import (
	"encoding/json"
	"net/http"
	"quiclab/internal/mdm"
	"strconv"
	"time"
)

func (w *Web) mdmState(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, false); !ok || !w.mdmReady(rw) {
		return
	}
	state, e := w.MDM.DeviceState(r.URL.Query().Get("id"))
	if e != nil {
		http.NotFound(rw, r)
		return
	}
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(state)
}
func mdmReply(rw http.ResponseWriter, value any, e error) {
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Type", "application/json")
	if e != nil {
		code := 400
		if e == mdm.ErrConflict {
			code = 409
		}
		if e == mdm.ErrUnauthorized {
			code = 403
		}
		if e == mdm.ErrPaused {
			code = 409
		}
		if e == mdm.ErrLimit {
			code = 429
		}
		http.Error(rw, http.StatusText(code), code)
		return
	}
	_ = json.NewEncoder(rw).Encode(value)
}
func (w *Web) mdmConfig(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok || !w.mdmReady(rw) {
		return
	}
	revision, e := strconv.ParseInt(r.Form.Get("expectedRevision"), 10, 64)
	if e != nil {
		mdmReply(rw, nil, mdm.ErrInvalid)
		return
	}
	generation, e := strconv.ParseInt(r.Form.Get("expectedGeneration"), 10, 64)
	if e != nil {
		mdmReply(rw, nil, mdm.ErrInvalid)
		return
	}
	out, e := w.MDM.SetDesiredChecked(r.Form.Get("id"), revision, generation, r.Form.Get("requestID"), r.Form.Get("mode"), json.RawMessage(r.Form.Get("document")))
	// Never echo the submitted keys, including on successful writes.
	mdmReply(rw, map[string]any{"revision": out.Revision}, e)
}
func (w *Web) mdmCommand(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok || !w.mdmReady(rw) {
		return
	}
	out, e := w.MDM.QueueVPNOnce(r.Form.Get("id"), r.Form.Get("kind"), r.Form.Get("requestID"), time.Now().UTC())
	mdmReply(rw, out, e)
}
