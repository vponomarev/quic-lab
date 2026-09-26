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
	m.HandleFunc("GET /{$}", w.home)
	m.HandleFunc("GET /download/quic-lab.apk", w.downloadAPK)
	m.HandleFunc("GET /login", w.loginPage)
	m.HandleFunc("POST /login", w.login)
	m.HandleFunc("POST /logout", w.logout)
	m.HandleFunc("GET /users", w.users)
	m.HandleFunc("GET /users/live", w.liveStats)
	m.HandleFunc("GET /users/uplink", w.uplink)
	m.HandleFunc("GET /stats.js", w.statsScript)
	m.HandleFunc("GET /ui.js", w.uiScript)
	m.HandleFunc("POST /users/add", w.add)
	m.HandleFunc("POST /users/rename", w.rename)
	m.HandleFunc("POST /users/protocols", w.protocols)
	m.HandleFunc("POST /users/toggle", w.toggle)
	m.HandleFunc("POST /users/awg-qr", w.awgQR)
	m.HandleFunc("POST /users/config", w.downloadProfile)
	m.HandleFunc("POST /users/delete", w.delete)
	m.HandleFunc("POST /users/qr", w.qr)
	m.HandleFunc("POST /enroll", w.enroll)
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
		rw.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'self'; connect-src 'self'; img-src data:; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		r.Body = http.MaxBytesReader(rw, r.Body, 16384)
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
	http.SetCookie(rw, &http.Cookie{Name: "quiclab_admin", Path: w.base, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	http.Redirect(rw, r, w.base, 303)
}
func qrImage(raw string) (template.URL, error) {
	b, e := qrcode.Encode(raw, qrcode.Medium, 512)
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(b)), e
}
func (w *Web) home(rw http.ResponseWriter, r *http.Request) {
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
	w.page(rw, view{Title: "Подключитесь к echo-стенду", Echo: true, APKSize: apkSize, APKQR: apkQR, QR: img, Endpoint: p.Endpoint, Hostname: p.Hostname})
}
func (w *Web) users(rw http.ResponseWriter, r *http.Request) {
	s, ok := w.authorized(rw, r, false)
	if !ok {
		return
	}
	w.page(rw, view{Title: "Пользователи VPN", Admin: true, CSRF: s.CSRF, Users: w.Store.List()})
}
func (w *Web) add(rw http.ResponseWriter, r *http.Request) {
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
		w.page(rw, view{Title: "Пользователи VPN", Admin: true, CSRF: s.CSRF, Users: w.Store.List(), Message: e.Error()})
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
		w.page(rw, view{Title: "Пользователи VPN", Admin: true, CSRF: session.CSRF, Users: w.Store.List(), Message: e.Error()})
		return
	}
	http.Redirect(rw, r, w.base+"users", http.StatusSeeOther)
}

func (w *Web) delete(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	id := r.Form.Get("id")
	if e := w.Store.Delete(id); e != nil {
		http.Error(rw, "Cannot delete user", 400)
		return
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
	s, ok := w.authorized(rw, r, true)
	if !ok {
		return
	}
	u, e := w.Store.Profile(r.Form.Get("id"))
	if e != nil {
		http.NotFound(rw, r)
		return
	}
	token := randomID()
	w.mu.Lock()
	w.prune()
	for k, v := range w.tickets {
		if v.User == u.ID {
			delete(w.tickets, k)
		}
	}
	w.tickets[token] = ticket{u.ID, time.Now().Add(5 * time.Minute)}
	w.mu.Unlock()
	img, e := qrImage(w.Config.PublicURL + "enroll#" + token)
	if e != nil {
		http.Error(rw, "QR unavailable", 500)
		return
	}
	w.page(rw, view{Title: "Профиль: " + u.Name, Admin: true, CSRF: s.CSRF, QR: img, Enrollment: true})
}
func (w *Web) enroll(rw http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Token) != 48 {
		http.Error(rw, "Invalid enrollment", 400)
		return
	}
	w.mu.Lock()
	w.prune()
	t, ok := w.tickets[body.Token]
	if ok {
		delete(w.tickets, body.Token)
	}
	w.mu.Unlock()
	if !ok {
		http.Error(rw, "QR expired or already used", 410)
		return
	}
	u, e := w.Store.Profile(t.User)
	if e != nil {
		http.Error(rw, "User no longer available", 410)
		return
	}
	p, e := w.profile(u)
	if e != nil {
		http.Error(rw, "Profile unavailable", 409)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(p)
}

type view struct {
	Entries                                        []entryPoint
	AWGNetwork                                     string
	TransitGateway                                 string
	AWGAvailable                                   bool
	AWGHealth                                      string
	AWGQR                                          bool
	APKQR                                          template.URL
	APKSize                                        string
	Title, Base, Message, CSRF, Endpoint, Hostname string
	Login, Echo, Admin, Enrollment                 bool
	QR                                             template.URL
	Users                                          []User
}

func (w *Web) page(rw http.ResponseWriter, v view) {
	v.Base = w.base
	if v.Admin {
		v.Entries = w.entryPoints()
	}
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

var page = template.Must(template.New("page").Parse(`<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}} · QUIC Lab</title><style>
.protocols{display:flex;gap:12px;flex-wrap:wrap}.protocols label{display:flex;align-items:center;gap:5px;margin:4px 0}.protocol-badges{display:flex;gap:5px;margin:8px 0}.protocol-badges span{background:#e9f4f2;color:#087769;border-radius:6px;padding:3px 6px;font-size:12px}.disabled{color:#a43e3e}.download-qr{margin-top:18px}.download-qr summary{display:inline-block;padding:10px 16px;border:1px solid #008075;border-radius:9px;color:#008075;cursor:pointer}.add-user{display:flex;align-items:center;gap:12px;flex-wrap:wrap}.add-user label{margin:0}.add-user #new-user-name{flex:1;min-width:180px;max-width:300px}.protocols input[type=checkbox]{flex:none;min-width:0;width:18px;height:18px;margin:0;padding:0;accent-color:#008075}.protocols label{white-space:nowrap}.export{margin:8px 4px}.export summary{cursor:pointer;color:#008075}.export form{display:block;margin:8px 0}.export button{font-size:14px}.stats-help{margin:16px 0}.stats-help summary{cursor:pointer}.download{display:inline-block;padding:12px 18px;background:#008075;color:white;border-radius:9px;text-decoration:none}body{margin:0;background:#f0f5f8;color:#14283a;font:16px system-ui,sans-serif}main{max-width:none;margin:40px auto;padding:0 24px}nav{display:flex;justify-content:space-between;align-items:center;margin-bottom:34px}a{color:#008075}h1{font-size:32px;margin:0 0 12px}.card{background:white;border-radius:20px;padding:28px;margin:20px 0;box-shadow:0 4px 20px #14283a08}.muted{color:#5e707f}input{font:inherit;padding:12px;border:1px solid #c4d1da;border-radius:9px;box-sizing:border-box;max-width:100%}button{font:inherit;padding:11px 16px;border:0;border-radius:9px;background:#008075;color:white;cursor:pointer}.danger{background:#a43e3e}.inline{display:inline-block;margin:4px}label{display:block;margin:16px 0 6px}img{width:min(100%,360px);height:auto;image-rendering:pixelated}.qr{text-align:center}.alert{background:#fff1d6;padding:14px;border-radius:9px}table{width:100%;border-collapse:collapse}td,th{text-align:left;padding:14px 8px;border-bottom:1px solid #edf1f4}.table{overflow:auto}.online{color:#008075}.traffic{white-space:nowrap}td{vertical-align:top}[data-stat="last"]{white-space:nowrap}.rename{margin-top:8px}.rename summary{color:#008075;cursor:pointer}.rename input{min-width:180px}small{color:#5e707f}code{word-break:break-all}@media(max-width:600px){main{margin:20px auto;padding:0 14px}.card{padding:18px}h1{font-size:26px}}.uplink{padding:16px 20px;display:flex;align-items:center;gap:20px;flex-wrap:wrap;border-left:4px solid #008075}.uplink>div{display:grid;gap:4px}.uplink small{font-size:12px}.uplink>p{width:100%;margin:0}.uplink>p:empty{display:none}.uplink #uplink-time{margin-left:auto}.user-ip{display:block;margin:3px 0}.user-actions{display:flex;align-items:flex-start;gap:8px;flex-wrap:wrap;min-width:230px}.user-actions button,.user-actions summary{font-size:13px;padding:7px 10px;border-radius:7px}.user-actions summary{list-style:none;background:#edf5f4;color:#087769;cursor:pointer}.user-actions summary::before{content:'▸ ';}.user-actions details[open]>summary::before{content:'▾ ';}.user-actions .inline,.user-actions .export,.user-actions .rename{margin:0}.user-actions details[open]{flex-basis:100%}.user-actions .settings-body{padding:12px;background:#f7fafb;border-radius:8px;max-width:340px}.user-actions .settings-body input{width:100%;min-width:0}.user-actions .settings-body label{margin-top:8px}.user-actions .settings-body p{margin:8px 0}.user-actions hr{border:0;border-top:1px solid #dce5e9;margin:12px 0}.protocol-badges{margin:5px 0 0;gap:4px}.protocol-badges span{padding:2px 5px}.card.table{padding:10px 18px}td,th{padding:10px 8px;font-size:14px}td p{margin:4px 0}td[data-stat="connections"]{min-width:170px}.card:has(.add-user){padding:16px 20px}.add-user input{padding:8px 10px}.add-user button{padding:9px 12px}.stats-help{margin:10px 0}nav{margin-bottom:22px}.card{margin:14px 0}@media(max-width:800px){.uplink{gap:14px}.uplink #uplink-time{margin-left:0}.add-user #new-user-name{max-width:none}.card.table{padding:8px}.user-actions{min-width:180px}}
.entry-points{padding:14px 20px}.entry-points summary{cursor:pointer;font-weight:600}.entry-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px 20px;margin-top:12px}.entry-grid>div{display:grid;gap:3px}.entry-grid code{font-size:13px;word-break:normal;overflow-wrap:anywhere}.entry-grid small{font-size:12px}@media(max-width:900px){.entry-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:500px){.entry-grid{grid-template-columns:1fr}}
.login-page{max-width:440px;margin:9vh auto}.login-page h1{font-size:28px}.login-page .card{padding:32px}.login-page input{width:100%}.login-page button{width:100%;margin-top:12px}.login-page label{font-size:14px}.login-page .muted{line-height:1.6}
.page-tools{display:flex;justify-content:flex-end;align-items:center;gap:12px;margin:12px 0}.info-button{background:transparent;color:#008075;border:1px solid #b9d7d3;border-radius:50%;width:32px;height:32px;padding:0;font-size:20px}.page-tools #live-status{font-size:12px}.user-actions{flex-wrap:nowrap;min-width:0}.user-actions>div{display:inline-block}.secondary{background:#edf5f4;color:#087769}
dialog{border:0;border-radius:20px;padding:0;width:min(540px,calc(100vw - 32px));max-height:85vh;box-shadow:0 24px 90px #14283a40;color:#14283a;background:white}dialog::backdrop{background:#14283a66;backdrop-filter:blur(3px)}.modal-header{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:22px 26px;border-bottom:1px solid #edf1f4}.modal-header h2{margin:0;font-size:21px}.modal-close{background:#edf5f4;color:#087769;font-size:23px;padding:3px 12px}.modal-body{padding:24px 26px;overflow-wrap:anywhere}.modal-body .settings-body{padding:0;background:none;max-width:none}.modal-body input:not([type=checkbox]):not([type=hidden]){width:100%;min-width:0}.modal-body .protocols input[type=checkbox]{width:18px}.modal-body .protocols{gap:18px}.modal-body label{margin-top:8px}.modal-body hr{border:0;border-top:1px solid #edf1f4;margin:22px 0}.modal-body .inline{display:block;margin:12px 0}.modal-body .export-grid{display:grid;gap:12px}.modal-body form button{font-size:14px;padding:10px 14px}.modal-body p{line-height:1.6}.modal-body .client-name{font-weight:600;margin:0 0 20px}.modal-body img{display:block;margin:auto}.modal-trigger{white-space:nowrap}@media(min-width:1600px){main{padding:0 40px}}
</style><script src="{{.Base}}ui.js" defer></script><main><nav><a href="{{.Base}}"><strong>QUIC LAB</strong></a>{{if .Admin}}<a href="{{.Base}}users">Пользователи</a><form method="post" action="{{.Base}}logout"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>Выйти</button></form>{{else}}<a href="{{.Base}}login">Вход администратора</a>{{end}}</nav><h1>{{.Title}}</h1>{{if .Message}}<p class="alert">{{.Message}}</p>{{end}}
{{if .Login}}<div class="login-page"><section class="card"><h2>Добро пожаловать</h2><p class="muted">Войдите, чтобы управлять пользователями и подключениями QUIC Lab.</p><form method="post" action="{{.Base}}login"><label for="login-user">Логин</label><input id="login-user" name="username" autofocus autocomplete="username" required><label for="login-password">Пароль</label><input id="login-password" type="password" name="password" autocomplete="current-password" required><p><button>Войти</button></p></form></section></div>{{end}}
{{if .Echo}}<p class="muted">Откройте QUIC Lab на телефоне и нажмите «Сканировать QR». Вход для echo не нужен.</p><section class="card qr"><img src="{{.QR}}" alt="QR настроек echo"><p><code>{{.Endpoint}}</code></p><small>TLS: {{.Hostname}}</small>{{if .APKSize}}<p><a class="download" href="{{.Base}}download/quic-lab.apk" download>Скачать QUIC Lab для Android · {{.APKSize}}</a></p><small>Android 11+ · ARM64 / x86-64. Установите APK, затем отсканируйте QR-код в приложении.</small><details class="download-qr"><summary>Показать QR для скачивания</summary><p>Сканируйте камерой телефона, чтобы скачать APK.</p><img src="{{.APKQR}}" alt="QR для скачивания APK QUIC Lab"></details>{{end}}</section>{{end}}
{{if .Enrollment}}<p class="muted">{{if .AWGQR}}AmneziaWG: отсканируйте QR в совместимом клиенте.{{else}}В QUIC Lab нажмите «Сканировать QR». После импорта доступны разрешённые администратором протоколы.{{end}}</p><section class="card qr"><img src="{{.QR}}" alt="Одноразовый QR профиля VPN"><p>{{if .AWGQR}}QR содержит конфигурацию и закрытый ключ AWG.{{else}}Одно использование · действует 5 минут. QR выдаёт конфигурацию разрешённых протоколов и их ключи.{{end}}</p><small>Передавайте только владельцу устройства.</small></section>{{end}}
{{if and .Admin (not .Enrollment)}}<section class="card entry-points"><details open><summary>Точки входа <small>· публичные адреса клиентов</small></summary><div class="entry-grid">{{range .Entries}}<div><small>{{.Service}} · {{.Network}}</small><code>{{.Address}}</code>{{if .Binding}}<small>Слушает: <code>{{.Binding}}</code></small>{{end}}{{if eq .Service "AmneziaWG"}}<small>Внутренняя сеть: <code>{{$.AWGNetwork}}</code> · {{$.AWGHealth}}</small>{{end}}</div>{{end}}</div></details></section>{{if .TransitGateway}}<section class="card uplink" id="uplink" data-url="{{.Base}}users/uplink"><div><small>Аплинк · AmneziaWG</small><strong id="uplink-state" role="status">Проверяем…</strong></div><div><small>Удалённый gateway</small><code>{{.TransitGateway}}</code></div><div><small>RTT · сервер → gateway</small><strong id="uplink-rtt">—</strong></div><div><small>Внешний IP выхода</small><strong id="uplink-exit">—</strong></div><small id="uplink-time">Обновление каждые 30 секунд</small><p id="uplink-error" class="muted"></p></section>{{end}}<div class="page-tools"><span id="live-status" class="muted" role="status">Подключение к обновлениям…</span><button class="info-button" data-help aria-label="Справка о статистике">ⓘ</button></div><dialog id="stats-help"><header class="modal-header"><h2>О лаборатории и статистике</h2><button class="modal-close" data-close aria-label="Закрыть">×</button></header><div class="modal-body"><p>Добавьте пользователя, затем покажите ему QR для импорта профиля.</p><p>TX ↑ — от клиента, RX ↓ — к клиенту. Окно 10 минут. QUIC/HTTPS: полезный трафик TCP/UDP. AWG: счётчики ядра протокола, включая служебный трафик; недавняя активность не гарантирует присутствие клиента онлайн. Скорость усреднена за 5 секунд. Счётчики обнуляются при перезапуске сервера, последнее подключение сохраняется. IP — адрес, видимый серверу; для QUIC меняется при миграции. Время UTC.</p><p>Статистика обновляется каждые 5 секунд. Изменения доступа AmneziaWG применяются в течение секунды.</p><a href="{{.Base}}users">Обновить страницу</a></div></dialog><script src="{{.Base}}stats.js" defer></script><section class="card"><form class="add-user" method="post" action="{{.Base}}users/add"><input type="hidden" name="csrf" value="{{.CSRF}}"><label for="new-user-name">Имя пользователя</label><input id="new-user-name" name="name" maxlength="100" required> <input type="hidden" name="protocols_present" value="1"><span class="protocols"><label><input type="checkbox" name="protocol" value="quic" checked> QUIC</label><label><input type="checkbox" name="protocol" value="https" checked> HTTPS</label>{{if .AWGAvailable}}<label><input type="checkbox" name="protocol" value="awg" checked> AmneziaWG</label>{{end}}</span><button>Добавить пользователя</button></form></section><section class="card table"><table><thead><tr><th>Пользователь</th><th>Подключения</th><th>Последнее подключение</th><th>Трафик · 10 мин</th><th>Доступ до</th><th>Действия</th></tr></thead><tbody id="user-stats" data-live-url="{{.Base}}users/live">{{range .Users}}<tr data-user-id="{{.ID}}"><td><strong data-stat="name">{{.Name}}</strong>{{if .Disabled}}<small class="disabled"> · выключен</small>{{end}}{{if .AWG}}<small class="user-ip">{{.AWG.Address}}</small>{{end}}<p class="protocol-badges">{{if .QUICEnabled}}<span>QUIC</span>{{end}}{{if .HTTPSEnabled}}<span>HTTPS</span>{{end}}{{if .AWGEnabled}}<span>AWG</span>{{end}}</p></td><td data-stat="connections">{{if .Stats.Connections}}<strong class="online">Онлайн · {{len .Stats.Connections}}</strong>{{range .Stats.Connections}}<p>{{.Transport}} · <code>{{.Source}}</code><br><small>С {{.Connected.Format "02.01 15:04:05"}} UTC</small></p>{{end}}{{else}}<span class="muted">Офлайн</span>{{end}}</td><td data-stat="last">{{if .LastConnected.IsZero}}<span class="muted">Ещё не подключался</span>{{else}}{{.LastConnected.Format "02.01.2006 15:04:05"}} UTC<br><small>{{.LastTransport}} · {{.LastSource}}</small>{{end}}</td><td class="traffic" data-stat="traffic">↑ {{.Stats.TXText}} · ↓ {{.Stats.RXText}}<br><small data-stat="rate">{{.Stats.RateText}}</small></td><td>{{.Expires.Format "02.01.2006"}}</td><td><div class="user-actions"><form class="inline" method="post" action="{{$.Base}}users/toggle"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="disabled" value="{{if .Disabled}}false{{else}}true{{end}}"><button role="switch" aria-checked="{{if .Disabled}}false{{else}}true{{end}}" class="{{if .Disabled}}danger{{end}}">{{if .Disabled}}Включить{{else}}Выключить{{end}}</button></form><details class="export"><summary>QR / конфиг</summary><form class="inline" method="post" action="{{$.Base}}users/qr"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><button>QR QUIC Lab</button></form><form class="inline" method="post" action="{{$.Base}}users/config"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><button>Конфиг QUIC Lab</button></form>{{if .AWGEnabled}}<form class="inline" method="post" action="{{$.Base}}users/awg-qr"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><button>QR AmneziaWG</button></form><form class="inline" method="post" action="{{$.Base}}users/config"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="format" value="awg"><button>Скачать .conf</button></form>{{end}}</details><details class="rename user-settings"><summary>Настройки</summary><div class="settings-body"><form method="post" action="{{$.Base}}users/rename"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><label for="name-{{.ID}}">Название клиента</label><input id="name-{{.ID}}" name="name" value="{{.Name}}" maxlength="100" required><p><button>Сохранить</button></p></form><hr><strong>Доступные протоколы</strong><form method="post" action="{{$.Base}}users/protocols"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><span class="protocols"><label><input type="checkbox" name="protocol" value="quic" {{if .QUICEnabled}}checked{{end}}> QUIC</label><label><input type="checkbox" name="protocol" value="https" {{if .HTTPSEnabled}}checked{{end}}> HTTPS</label>{{if $.AWGAvailable}}<label><input type="checkbox" name="protocol" value="awg" {{if .AWGEnabled}}checked{{end}}> AmneziaWG</label>{{end}}</span><p><button>Сохранить доступ</button></p></form><hr><form class="inline" method="post" action="{{$.Base}}users/delete"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><button class="danger">Удалить и отключить</button></form></div></details></div></td></tr>{{else}}<tr><td colspan="6" class="muted">Пользователей пока нет.</td></tr>{{end}}</tbody></table></section>{{end}}<p class="muted">QUIC Lab · Echo / VPN / TLS</p></main></html>`))
