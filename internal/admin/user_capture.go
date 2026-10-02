package admin

import (
	"encoding/json"
	"net/http"
	"time"
)

func (w *Web) userCapture(rw http.ResponseWriter, r *http.Request) {
	w.userCaptureMu.Lock()
	defer w.userCaptureMu.Unlock()
	owner, ok := w.authorized(rw, r, true)
	if !ok {
		return
	}
	if w.Capture == nil {
		http.Error(rw, "Capture disabled", 503)
		return
	}
	id := r.Form.Get("id")
	var user *User
	for _, u := range w.Store.List() {
		if u.ID == id {
			v := u
			user = &v
			break
		}
	}
	if user == nil {
		http.NotFound(rw, r)
		return
	}
	action := r.Form.Get("action")
	s := w.Capture.UserCapture(id)
	if s != nil && s.Snapshot().Owner != owner.CSRF {
		http.Error(rw, "Capture belongs to another login", 403)
		return
	}
	if action == "stop" {
		if s != nil {
			s.Stop("stopped by teacher")
		}
		if wantsCaptureJSON(r) {
			captureJSON(rw, map[string]bool{"ok": true})
			return
		}
		http.Redirect(rw, r, w.base+"users", 303)
		return
	}
	if action != "start" && action != "link" {
		http.Error(rw, "Unknown action", 400)
		return
	}
	if e := w.Store.CaptureEligible(id); e != nil {
		http.Error(rw, e.Error(), 409)
		return
	}
	if s == nil {
		if action == "link" {
			http.Error(rw, "Capture expired; start a new one", 410)
			return
		}
		if user.Disabled || time.Now().After(user.Expires) {
			http.Error(rw, "User access disabled or expired", 409)
			return
		}
		iface, ip := "", ""
		if w.Config.AWG != nil && user.AWG != nil && user.AWGEnabled() {
			iface = w.Config.AWG.Interface
			ip = user.AWG.Address
		}
		var e error
		w.Store.ConfigureCapture(w.Capture)
		e = w.Store.WithCaptureEligible(id, func() error {
			var err error
			s, err = w.Capture.StartUser(owner.CSRF, id, iface, ip)
			return err
		})
		if e != nil {
			http.Error(rw, e.Error(), 409)
			return
		}
	}
	r.Form.Set("id", s.ID)
	w.captureLaunch(rw, r)
}
func (w *Web) userCaptureStates(rw http.ResponseWriter, r *http.Request) {
	owner, ok := w.authorized(rw, r, false)
	if !ok {
		return
	}
	rows := map[string]map[string]any{}
	if w.Capture != nil {
		for _, u := range w.Store.List() {
			if s := w.Capture.UserCapture(u.ID); s != nil {
				v := s.Snapshot()
				rows[u.ID] = map[string]any{"owned": v.Owner == owner.CSRF, "bytes": v.Bytes, "until": v.Until.Unix()}
			}
		}
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(rows)
}
func (w *Web) captureUserScript(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	rw.Write(captureUsersJS)
}
