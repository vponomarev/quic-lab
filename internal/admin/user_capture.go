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
		http.Redirect(rw, r, w.base+"users", 303)
		return
	}
	if action != "start" && action != "link" {
		http.Error(rw, "Unknown action", 400)
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
		s, e = w.Capture.StartUser(owner.CSRF, id, iface, ip)
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
	rw.Header().Set("Content-Type", "application/javascript")
	rw.Write([]byte(`
(function(){
 const panels=document.querySelectorAll('.user-capture');if(!panels.length)return;
 const base=panels[0].dataset.base;
 async function refresh(){try{
  const r=await fetch(base+'users/captures',{credentials:'same-origin'});if(!r.ok)return;const states=await r.json();
  panels.forEach(p=>{const s=states[p.dataset.user];p.querySelector('.capture-start').hidden=!!s;p.querySelector('.capture-active').hidden=!s;
   if(s){p.querySelector('.capture-label').textContent='● Захват включён · '+Math.round(s.bytes/1024)+' KiB'+(s.owned?'':' · другой вход');
    p.querySelectorAll('.capture-owned').forEach(f=>f.hidden=!s.owned);}
  });
 }catch(e){} }
 refresh();setInterval(refresh,5000);
})();`))
}
