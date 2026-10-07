package admin

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"github.com/skip2/go-qrcode"
	"html/template"
	"net/http"
	"net/url"
	"quiclab/internal/debugcapture"
	"strconv"
	"strings"
	"sync"
	"time"
)

type session struct {
	CSRF  string
	Until time.Time
}
type ticket struct {
	User  string
	Until time.Time
}
type Web struct {
	diagnosticSync func(string) error
	// Serialize capture creation with identity changes and AWG address reuse.
	userCaptureMu    sync.Mutex
	Capture          *debugcapture.Manager
	PublicAWG        http.Handler
	ListenerBindings map[string]string
	uplinkState      uplinkCache
	Config           Config
	Store            *Store
	mu               sync.Mutex
	sessions         map[string]session
	tickets          map[string]ticket
	attempts         int
	window           time.Time
	base, origin     string
}

func NewWeb(c Config, s *Store) *Web {
	u, _ := url.Parse(c.PublicURL)
	w := &Web{Config: c, Store: s, sessions: make(map[string]session), tickets: make(map[string]ticket), base: u.Path, origin: u.Scheme + "://" + u.Host}
	w.loadSessions()
	return w
}
func (w *Web) Handler() http.Handler {
	m := http.NewServeMux()
	w.captureRoutes(m)
	m.HandleFunc("GET /{$}", w.home)
	m.HandleFunc("GET /download/quic-lab.apk", w.downloadAPK)
	m.HandleFunc("GET /login", w.loginPage)
	m.HandleFunc("POST /login", w.login)
	m.HandleFunc("POST /logout", w.logout)
	m.HandleFunc("GET /users", w.users)
	m.HandleFunc("POST /users/capture", w.userCapture)
	m.HandleFunc("GET /users/captures", w.userCaptureStates)
	m.HandleFunc("GET /capture-users.js", w.captureUserScript)
	m.HandleFunc("GET /users/live", w.liveStats)
	m.HandleFunc("GET /users/uplink", w.uplink)
	m.HandleFunc("GET /stats.js", w.statsScript)
	m.HandleFunc("GET /ui.js", w.uiScript)
	m.HandleFunc("GET /ui.css", w.uiStyle)
	m.HandleFunc("GET /diagnostics/echo", w.echoHome)
	m.HandleFunc("GET /diagnostics/clients", w.clientDiagnosticsPage)
	m.HandleFunc("POST /api/v1/devices/{id}/diagnostics", w.clientDiagnostics)
	m.HandleFunc("POST /users/settings", w.userSettings)
	m.HandleFunc("POST /users/add", w.add)
	m.HandleFunc("POST /users/rename", w.rename)
	m.HandleFunc("POST /users/protocols", w.protocols)
	m.HandleFunc("POST /users/toggle", w.toggle)
	m.HandleFunc("POST /devices/disable", w.disableDevice)
	m.HandleFunc("POST /users/awg-qr", w.awgQR)
	m.HandleFunc("POST /users/vless-qr", w.vlessQR)
	m.HandleFunc("GET /vless", w.vlessSettings)
	m.HandleFunc("GET /vless-settings.js", w.vlessSettingsScript)
	m.HandleFunc("POST /vless/config", w.vlessConfigure)
	for _, path := range []string{"generate-key", "generate-short-id", "check-target"} {
		m.HandleFunc("POST /vless/"+path, w.vlessTool)
	}
	m.HandleFunc("POST /users/config", w.downloadProfile)
	m.HandleFunc("POST /users/delete", w.delete)
	m.HandleFunc("POST /users/qr", w.qr)
	m.HandleFunc("POST /enroll", w.enroll)
	m.HandleFunc("GET /api/v1/devices/{id}/config", w.deviceConfig)
	m.HandleFunc("POST /enrollments/revoke", w.revokeEnrollment)
	m.HandleFunc("POST /enrollments/show", w.showEnrollment)
	m.HandleFunc("POST /echo/awg", func(rw http.ResponseWriter, r *http.Request) {
		if w.PublicAWG == nil {
			http.NotFound(rw, r)
			return
		}
		w.PublicAWG.ServeHTTP(rw, r)
	})
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Cache-Control", "no-store")
		rw.Header().Set("Pragma", "no-cache")
		rw.Header().Set("X-Content-Type-Options", "nosniff")
		// Preserve Origin on same-origin form POSTs; disclose no referrer cross-origin.
		rw.Header().Set("Referrer-Policy", "same-origin")
		rw.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; img-src data:; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		limit := int64(16384)
		if r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/api/v1/devices/") && strings.HasSuffix(r.URL.Path, "/diagnostics") {
			limit = 262144
		}
		r.Body = http.MaxBytesReader(rw, r.Body, limit)
		m.ServeHTTP(rw, r)
	})
}
func (w *Web) prune() {
	now := time.Now()
	for k, v := range w.sessions {
		if now.After(v.Until) {
			delete(w.sessions, k)
		}
	}
	for k, v := range w.tickets {
		if now.After(v.Until) {
			delete(w.tickets, k)
		}
	}
}
func (w *Web) getSession(r *http.Request) (session, bool) {
	c, e := r.Cookie("quiclab_admin")
	if e != nil {
		return session{}, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.prune()
	s, ok := w.sessions[c.Value]
	return s, ok
}
func (w *Web) authorized(rw http.ResponseWriter, r *http.Request, post bool) (session, bool) {
	s, ok := w.getSession(r)
	if !ok {
		http.Redirect(rw, r, w.base+"login", http.StatusSeeOther)
		return s, false
	}
	if post && (r.Header.Get("Origin") != w.origin || r.ParseForm() != nil || subtle.ConstantTimeCompare([]byte(r.Form.Get("csrf")), []byte(s.CSRF)) != 1) {
		http.Error(rw, "Invalid form; reload the page", 403)
		return s, false
	}
	return s, true
}
func (w *Web) loginPage(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.getSession(r); ok {
		http.Redirect(rw, r, w.base+"users", http.StatusSeeOther)
		return
	}
	w.page(rw, view{Title: "Вход администратора", Login: true})
}
func (w *Web) login(rw http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != w.origin || r.ParseForm() != nil {
		http.Error(rw, "Invalid origin", 403)
		return
	}
	w.mu.Lock()
	if time.Since(w.window) > time.Minute {
		w.window = time.Now()
		w.attempts = 0
	}
	w.attempts++
	limited := w.attempts > 20
	w.mu.Unlock()
	if limited {
		http.Error(rw, "Too many attempts; retry in a minute", 429)
		return
	}
	given := sha256.Sum256([]byte(r.Form.Get("username") + "\x00" + r.Form.Get("password")))
	want := sha256.Sum256([]byte(w.Config.Username + "\x00" + w.Config.Password))
	if subtle.ConstantTimeCompare(given[:], want[:]) != 1 {
		rw.WriteHeader(401)
		w.page(rw, view{Title: "Вход администратора", Login: true, Message: "Неверный логин или пароль"})
		return
	}
	token := randomID()
	w.mu.Lock()
	w.prune()
	if len(w.sessions) >= 128 {
		w.mu.Unlock()
		http.Error(rw, "Session limit", 429)
		return
	}
	w.sessions[token] = session{randomID(), time.Now().Add(8 * time.Hour)}
	if err := w.saveSessions(); err != nil {
		delete(w.sessions, token)
		w.mu.Unlock()
		http.Error(rw, "Session storage unavailable", 503)
		return
	}
	w.mu.Unlock()
	http.SetCookie(rw, &http.Cookie{Name: "quiclab_admin", Value: token, Path: w.base, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
	http.Redirect(rw, r, w.base+"users", 303)
}
func (w *Web) logout(rw http.ResponseWriter, r *http.Request) {
	w.userCaptureMu.Lock()
	defer w.userCaptureMu.Unlock()
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	c, _ := r.Cookie("quiclab_admin")
	w.mu.Lock()
	previous := w.sessions[c.Value]
	delete(w.sessions, c.Value)
	if err := w.saveSessions(); err != nil {
		w.sessions[c.Value] = previous
		w.mu.Unlock()
		http.Error(rw, "Session storage unavailable", 503)
		return
	}
	w.mu.Unlock()
	if w.Capture != nil {
		w.Capture.StopOwner(previous.CSRF)
	}
	http.SetCookie(rw, &http.Cookie{Name: "quiclab_admin", Path: w.base, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	http.Redirect(rw, r, w.base, 303)
}
func qrImage(raw string) (template.URL, error) {
	b, e := qrcode.Encode(raw, qrcode.Medium, 512)
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(b)), e
}
func (w *Web) echoHome(rw http.ResponseWriter, r *http.Request) {
	auth, ok := w.authorized(rw, r, false)
	if !ok {
		return
	}
	p := w.Config.Echo
	if w.PublicAWG != nil {
		p.AWGEnrollURL = w.Config.PublicURL + "echo/awg"
	}
	p.Version = 1
	p.Kind = "echo"
	b, _ := json.Marshal(p)
	img, e := qrImage(string(b))
	if e != nil {
		http.Error(rw, "QR unavailable", 500)
		return
	}
	apkSize := w.apkSize()
	var apkQR template.URL
	if apkSize != "" {
		apkQR, e = qrImage(w.Config.PublicURL + "download/quic-lab.apk")
		if e != nil {
			http.Error(rw, "Download QR unavailable", 500)
			return
		}
	}
	w.page(rw, view{Title: "Подключитесь к echo-стенду", Echo: true, Admin: true, CSRF: auth.CSRF, APKSize: apkSize, APKQR: apkQR, QR: img, Endpoint: p.Endpoint, Hostname: p.Hostname})
}
func (w *Web) users(rw http.ResponseWriter, r *http.Request) {
	s, ok := w.authorized(rw, r, false)
	if !ok {
		return
	}
	w.page(rw, view{Title: "Пользователи", Admin: true, CSRF: s.CSRF, Users: w.Store.List()})
}
func (w *Web) add(rw http.ResponseWriter, r *http.Request) {
	w.userCaptureMu.Lock()
	defer w.userCaptureMu.Unlock()
	s, ok := w.authorized(rw, r, true)
	if !ok {
		return
	}
	var protocols []string
	if r.Form.Get("protocols_present") == "1" {
		protocols = append([]string{}, r.Form["protocol"]...)
	}
	_, e := w.Store.CreateWithProtocols(strings.TrimSpace(r.Form.Get("name")), protocols)
	if e != nil {
		rw.WriteHeader(400)
		w.page(rw, view{Title: "Пользователи", Admin: true, CSRF: s.CSRF, Users: w.Store.List(), Message: e.Error()})
		return
	}
	http.Redirect(rw, r, w.base+"users", 303)
}
func (w *Web) rename(rw http.ResponseWriter, r *http.Request) {
	session, ok := w.authorized(rw, r, true)
	if !ok {
		return
	}
	if e := w.Store.Rename(r.Form.Get("id"), strings.TrimSpace(r.Form.Get("name"))); e != nil {
		rw.WriteHeader(http.StatusBadRequest)
		w.page(rw, view{Title: "Пользователи", Admin: true, CSRF: session.CSRF, Users: w.Store.List(), Message: e.Error()})
		return
	}
	http.Redirect(rw, r, w.base+"users", http.StatusSeeOther)
}

func (w *Web) delete(rw http.ResponseWriter, r *http.Request) {
	w.userCaptureMu.Lock()
	defer w.userCaptureMu.Unlock()
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	id := r.Form.Get("id")
	if e := w.Store.Delete(id); e != nil {
		http.Error(rw, "Cannot delete user", 400)
		return
	}
	if w.Capture != nil {
		w.Capture.StopUser(id)
	}
	w.mu.Lock()
	for k, v := range w.tickets {
		if v.User == id {
			delete(w.tickets, k)
		}
	}
	w.mu.Unlock()
	http.Redirect(rw, r, w.base+"users", 303)
}
func (w *Web) qr(rw http.ResponseWriter, r *http.Request) {
	session, ok := w.authorized(rw, r, true)
	if !ok {
		return
	}
	ttl := DefaultEnrollmentTTL
	limit := DefaultEnrollmentLimit
	if raw := r.Form.Get("ttl_hours"); raw != "" {
		hours, e := strconv.Atoi(raw)
		if e != nil {
			http.Error(rw, "Invalid TTL", 400)
			return
		}
		ttl = time.Duration(hours) * time.Hour
	}
	if raw := r.Form.Get("max_devices"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil {
			http.Error(rw, "Invalid limit", 400)
			return
		}
		limit = n
	}
	en, token, e := w.Store.NewEnrollment(r.Form.Get("id"), ttl, limit)
	if e != nil {
		http.Error(rw, e.Error(), 400)
		return
	}
	link := w.Config.PublicURL + "enroll#" + token
	img, e := qrImage(link)
	if e != nil {
		http.Error(rw, "QR unavailable", 500)
		return
	}
	w.page(rw, view{Title: "Регистрация устройств", Admin: true, CSRF: session.CSRF, QR: img, ConnectionLink: link, Enrollment: true, EnrollmentRecord: &en})
}
func (w *Web) revokeEnrollment(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	if e := w.Store.RevokeEnrollment(r.Form.Get("id")); e != nil {
		http.Error(rw, e.Error(), 400)
		return
	}
	http.Redirect(rw, r, w.base+"users", 303)
}
func (w *Web) enroll(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(rw, r.Body, 4096)
	var body struct {
		Token      string `json:"token"`
		RequestID  string `json:"request_id"`
		DeviceName string `json:"device_name"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil {
		http.Error(rw, "Invalid enrollment", 400)
		return
	}
	d, updateToken, e := w.Store.EnrollWithUpdate(body.Token, body.RequestID, body.DeviceName)
	if e != nil {
		http.Error(rw, "Enrollment unavailable", 410)
		return
	}
	u, e := w.Store.enrollmentProfileUser(d)
	if e != nil {
		http.Error(rw, "Device unavailable", 410)
		return
	}
	p, e := w.profile(u)
	if e != nil {
		http.Error(rw, "Profile unavailable", 409)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	raw, _ := json.Marshal(p)
	response := map[string]any{}
	json.Unmarshal(raw, &response)
	response["device_id"] = d.ID
	response["config_url"] = w.deviceConfigURL(d.ID)
	response["update_token"] = updateToken
	if w.Config.Capabilities.APKURL != "" {
		response["apk_url"] = w.Config.Capabilities.APKURL
	}
	json.NewEncoder(rw).Encode(response)
}

type view struct {
	DeviceReports                                       map[string]string
	DeviceVersions                                      map[string]string
	ClientVersionCounts                                 map[string]int
	ConnectionLink                                      string
	VisitorIP, VisitorAgent, VisitorLanguage            string
	TLSVersion, TLSCipher, TLSALPN, TLSSNI, VisitorHTTP string
	EnrollmentRecord                                    *Enrollment
	CaptureAvailable                                    bool
	Entries                                             []entryPoint
	AWGNetwork                                          string
	TransitGateway                                      string
	AWGAvailable                                        bool
	VLESSAvailable                                      bool
	VLESSQR                                             bool
	AWGHealth                                           string
	AWGQR                                               bool
	APKQR                                               template.URL
	APKSize                                             string
	Title, Base, Message, CSRF, Endpoint, Hostname      string
	Home, Login, Echo, Admin, Enrollment                bool
	QR                                                  template.URL
	Users                                               []User
}

func (w *Web) page(rw http.ResponseWriter, v view) {
	v.Base = w.base
	v.DeviceReports = w.deviceReportTimes(v.Users)
	v.DeviceVersions, v.ClientVersionCounts = w.deviceVersions(v.Users)
	if v.ConnectionLink != "" {
		rw.Header().Set("Cache-Control", "no-store")
		rw.Header().Set("Referrer-Policy", "no-referrer")
	}
	v.CaptureAvailable = w.Capture != nil
	if v.Admin {
		v.Entries = w.entryPoints()
	}
	v.VLESSAvailable, _, _ = w.Store.VLESSStatus()
	v.AWGAvailable = w.Config.AWG != nil
	if v.AWGAvailable {
		v.AWGHealth = w.Store.AWGHealth()
		v.AWGNetwork = w.Config.AWG.Address
	}
	if w.Config.Transit != nil {
		v.TransitGateway = w.Config.Transit.Endpoint
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	page.Execute(rw, v)
}

var page = template.Must(template.New("page").Parse(pageHTML))

func (w *Web) showEnrollment(rw http.ResponseWriter, r *http.Request) {
	session, ok := w.authorized(rw, r, true)
	if !ok {
		return
	}
	rw.Header().Set("Cache-Control", "no-store")
	en, token, err := w.Store.EnrollmentInvitation(r.Form.Get("id"))
	if err != nil {
		http.Error(rw, "Приглашение недоступно: истекло, отозвано, исчерпано или создано без сохранения QR.", http.StatusGone)
		return
	}
	link := w.Config.PublicURL + "enroll#" + token
	img, err := qrImage(link)
	if err != nil {
		http.Error(rw, "QR unavailable", 500)
		return
	}
	w.page(rw, view{Title: "Приглашение", Admin: true, CSRF: session.CSRF, QR: img, ConnectionLink: link, Enrollment: true, EnrollmentRecord: &en})
}
