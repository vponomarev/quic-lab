package admin

import (
	"encoding/json"
	"net/http"
	"time"
)

func (w *Web) protocols(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	id := r.Form.Get("id")
	// Disabled clients can be edited without accidentally enabling them.
	var found *User
	for _, u := range w.Store.List() {
		if u.ID == id {
			v := u
			found = &v
			break
		}
	}
	if found == nil {
		http.NotFound(rw, r)
		return
	}
	if e := w.Store.SetProtocols(id, append([]string{}, r.Form["protocol"]...), found.Disabled); e != nil {
		http.Error(rw, e.Error(), 400)
		return
	}
	w.revokeTickets(id)
	http.Redirect(rw, r, w.base+"users", 303)
}
func (w *Web) toggle(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	id := r.Form.Get("id")
	v := r.Form.Get("disabled")
	if v != "true" && v != "false" {
		http.Error(rw, "Invalid state", 400)
		return
	}
	for _, u := range w.Store.List() {
		if u.ID == id {
			p := u.Protocols
			if p == nil {
				p = []string{"quic", "https"}
			}
			if e := w.Store.SetProtocols(id, p, v == "true"); e != nil {
				http.Error(rw, e.Error(), 400)
				return
			}
			w.revokeTickets(id)
			http.Redirect(rw, r, w.base+"users", 303)
			return
		}
	}
	http.NotFound(rw, r)
}
func (w *Web) revokeTickets(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for k, t := range w.tickets {
		if t.User == id {
			delete(w.tickets, k)
		}
	}
}
func (w *Web) profile(u User) (Profile, error) {
	p := w.Config.VPN
	p.Version = 1
	p.Kind = "vpn"
	if w.Config.Transit != nil {
		p.TransitEndpoint = w.Config.Transit.Endpoint
	}
	p.Name = u.Name
	p.Certificate = u.Certificate
	p.Key = u.Key
	if u.Protocols != nil {
		p.Version = 2
		p.Transports = append([]string{}, u.Protocols...)
	}
	if !u.Allows("quic") {
		p.QUIC = ""
	}
	if !u.Allows("https") {
		p.HTTPS = ""
	}
	if !u.Allows("quic") && !u.Allows("https") {
		p.Certificate = ""
		p.Key = ""
	}
	if u.Allows("awg") {
		raw, e := w.Store.AWGProfile(u.ID)
		if e != nil {
			return Profile{}, e
		}
		p.AWGConfig = raw
	}
	return p, nil
}
func (w *Web) awgQR(rw http.ResponseWriter, r *http.Request) {
	s, ok := w.authorized(rw, r, true)
	if !ok {
		return
	}
	raw, e := w.Store.AWGProfile(r.Form.Get("id"))
	if e != nil {
		http.Error(rw, e.Error(), 400)
		return
	}
	img, e := qrImage(raw)
	if e != nil {
		http.Error(rw, "QR unavailable", 500)
		return
	}
	w.page(rw, view{Title: "AmneziaWG", Admin: true, CSRF: s.CSRF, QR: img, Enrollment: true, AWGQR: true})
}
func (w *Web) downloadProfile(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	u, e := w.Store.Profile(r.Form.Get("id"))
	if e != nil {
		http.NotFound(rw, r)
		return
	}
	if r.Form.Get("format") == "awg" {
		raw, e := w.Store.AWGProfile(u.ID)
		if e != nil {
			http.Error(rw, e.Error(), 400)
			return
		}
		rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
		rw.Header().Set("Content-Disposition", `attachment; filename="quic-lab-awg.conf"`)
		rw.Write([]byte(raw))
		return
	}
	p, e := w.profile(u)
	if e != nil {
		http.Error(rw, "Profile unavailable", 409)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Content-Disposition", `attachment; filename="quic-lab-profile.json"`)
	json.NewEncoder(rw).Encode(p)
}
func (s *Store) AWGHealth() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.awgUpdated.IsZero() || time.Since(s.awgUpdated) > 5*time.Second {
		return "нет свежего ответа процесса AWG"
	}
	return "работает · изменения доступа применяются в течение секунды"
}
