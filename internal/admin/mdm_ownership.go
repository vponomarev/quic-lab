package admin

import (
	"net/http"
	"net/url"
	"time"
)

func (w *Web) mdmUserExists(id string) bool {
	if id == "" {
		return true
	}
	for _, u := range w.Store.List() {
		if u.ID == id {
			return true
		}
	}
	return false
}
func (w *Web) mdmAssign(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok || !w.mdmReady(rw) {
		return
	}
	w.userCaptureMu.Lock()
	defer w.userCaptureMu.Unlock()
	if !w.mdmUserExists(r.Form.Get("userID")) {
		http.Error(rw, "Unknown user", 400)
		return
	}
	if e := w.MDM.AssignUser(r.Form.Get("id"), r.Form.Get("userID"), time.Now().UTC()); e != nil {
		http.Error(rw, "Cannot assign device", 400)
		return
	}
	http.Redirect(rw, r, w.base+"mdm?device="+url.QueryEscape(r.Form.Get("id")), 303)
}
