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
	Config       Config
	Store        *Store
	mu           sync.Mutex
	sessions     map[string]session
	tickets      map[string]ticket
	attempts     int
	window       time.Time
	base, origin string
}

func NewWeb(c Config, s *Store) *Web {
	u, _ := url.Parse(c.PublicURL)
	return &Web{Config: c, Store: s, sessions: make(map[string]session), tickets: make(map[string]ticket), base: u.Path, origin: u.Scheme + "://" + u.Host}
}
func (w *Web) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /{$}", w.home)
	m.HandleFunc("GET /login", w.loginPage)
	m.HandleFunc("POST /login", w.login)
	m.HandleFunc("POST /logout", w.logout)
	m.HandleFunc("GET /users", w.users)
	m.HandleFunc("GET /users/live", w.liveStats)
	m.HandleFunc("GET /stats.js", w.statsScript)
	m.HandleFunc("POST /users/add", w.add)
	m.HandleFunc("POST /users/rename", w.rename)
	m.HandleFunc("POST /users/delete", w.delete)
	m.HandleFunc("POST /users/qr", w.qr)
	m.HandleFunc("POST /enroll", w.enroll)
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
	delete(w.sessions, c.Value)
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
	p.Version = 1
	p.Kind = "echo"
	b, _ := json.Marshal(p)
	img, e := qrImage(string(b))
	if e != nil {
		http.Error(rw, "QR unavailable", 500)
		return
	}
	w.page(rw, view{Title: "Подключитесь к echo-стенду", Echo: true, QR: img, Endpoint: p.Endpoint, Hostname: p.Hostname})
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
	_, e := w.Store.Create(strings.TrimSpace(r.Form.Get("name")))
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
	p := w.Config.VPN
	p.Version = 1
	p.Kind = "vpn"
	p.Name = u.Name
	p.Certificate = u.Certificate
	p.Key = u.Key
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(p)
}

type view struct {
	Title, Base, Message, CSRF, Endpoint, Hostname string
	Login, Echo, Admin, Enrollment                 bool
	QR                                             template.URL
	Users                                          []User
}

func (w *Web) page(rw http.ResponseWriter, v view) {
	v.Base = w.base
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	page.Execute(rw, v)
}

var page = template.Must(template.New("page").Parse(`<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}} · QUIC Lab</title><style>
body{margin:0;background:#f0f5f8;color:#14283a;font:16px system-ui,sans-serif}main{max-width:1400px;margin:40px auto;padding:0 24px}nav{display:flex;justify-content:space-between;align-items:center;margin-bottom:34px}a{color:#008075}h1{font-size:32px;margin:0 0 12px}.card{background:white;border-radius:20px;padding:28px;margin:20px 0;box-shadow:0 4px 20px #14283a08}.muted{color:#5e707f}input{font:inherit;padding:12px;border:1px solid #c4d1da;border-radius:9px;box-sizing:border-box;max-width:100%}button{font:inherit;padding:11px 16px;border:0;border-radius:9px;background:#008075;color:white;cursor:pointer}.danger{background:#a43e3e}.inline{display:inline-block;margin:4px}label{display:block;margin:16px 0 6px}img{width:min(100%,360px);height:auto;image-rendering:pixelated}.qr{text-align:center}.alert{background:#fff1d6;padding:14px;border-radius:9px}table{width:100%;border-collapse:collapse}td,th{text-align:left;padding:14px 8px;border-bottom:1px solid #edf1f4}.table{overflow:auto}.online{color:#008075}.traffic{white-space:nowrap}td{vertical-align:top}[data-stat="last"]{white-space:nowrap}.rename{margin-top:8px}.rename summary{color:#008075;cursor:pointer}.rename input{min-width:180px}small{color:#5e707f}code{word-break:break-all}@media(max-width:600px){main{margin:20px auto;padding:0 14px}.card{padding:18px}h1{font-size:26px}}</style><main><nav><a href="{{.Base}}"><strong>QUIC LAB</strong></a>{{if .Admin}}<a href="{{.Base}}users">Пользователи</a><form method="post" action="{{.Base}}logout"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>Выйти</button></form>{{else}}<a href="{{.Base}}login">Вход администратора</a>{{end}}</nav><h1>{{.Title}}</h1>{{if .Message}}<p class="alert">{{.Message}}</p>{{end}}
{{if .Login}}<section class="card"><form method="post" action="{{.Base}}login"><label>Логин</label><input name="username" autocomplete="username" required><label>Пароль</label><input type="password" name="password" autocomplete="current-password" required><p><button>Войти</button></p></form></section>{{end}}
{{if .Echo}}<p class="muted">Откройте QUIC Lab на телефоне и нажмите «Сканировать QR». Вход для echo не нужен.</p><section class="card qr"><img src="{{.QR}}" alt="QR настроек echo"><p><code>{{.Endpoint}}</code></p><small>TLS: {{.Hostname}}</small></section>{{end}}
{{if .Enrollment}}<p class="muted">В приложении нажмите «Сканировать QR». После импорта можно выбрать QUIC или HTTPS.</p><section class="card qr"><img src="{{.QR}}" alt="Одноразовый QR профиля VPN"><p>Одно использование · действует 5 минут</p><small>QR выдаёт настройки, клиентский сертификат и закрытый ключ. Передавайте его только владельцу устройства. Новый QR отменяет предыдущий.</small></section>{{end}}
{{if and .Admin (not .Enrollment)}}<p class="muted">Добавьте пользователя, затем покажите ему QR для импорта профиля.</p><p class="muted">TX ↑ — от клиента, RX ↓ — к клиенту. Скользящее окно 10 минут; TCP и DNS (включая обрамление DNS), без keep-alive и транспортных накладных расходов. Счётчики обнуляются при перезапуске сервера, последнее подключение сохраняется. IP — адрес, видимый серверу; для QUIC меняется при миграции. Время UTC.</p><p><span id="live-status" class="muted" role="status">Подключение к обновлениям…</span> · <a href="{{.Base}}users">Обновить страницу</a></p><script src="{{.Base}}stats.js" defer></script><section class="card"><form method="post" action="{{.Base}}users/add"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Имя пользователя / устройства</label><input name="name" maxlength="100" placeholder="Телефон студента" required> <button>Добавить пользователя</button></form></section><section class="card table"><table><thead><tr><th>Пользователь</th><th>Подключения</th><th>Последнее подключение</th><th>Трафик · 10 мин</th><th>Сертификат до</th><th>Действия</th></tr></thead><tbody id="user-stats" data-live-url="{{.Base}}users/live">{{range .Users}}<tr data-user-id="{{.ID}}"><td><strong data-stat="name">{{.Name}}</strong><details class="rename"><summary>Изменить</summary><form method="post" action="{{$.Base}}users/rename"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><label for="name-{{.ID}}">Название клиента</label><input id="name-{{.ID}}" name="name" value="{{.Name}}" maxlength="100" required><p><button>Сохранить</button></p></form></details></td><td data-stat="connections">{{if .Stats.Connections}}<strong class="online">Онлайн · {{len .Stats.Connections}}</strong>{{range .Stats.Connections}}<p>{{.Transport}}<br><code>{{.Source}}</code><br><small>С {{.Connected.Format "02.01 15:04:05"}} UTC</small></p>{{end}}{{else}}<span class="muted">Офлайн</span>{{end}}</td><td data-stat="last">{{if .LastConnected.IsZero}}<span class="muted">Ещё не подключался</span>{{else}}{{.LastConnected.Format "02.01.2006 15:04:05"}} UTC<br><small>{{.LastTransport}} · {{.LastSource}}</small>{{end}}</td><td class="traffic" data-stat="traffic">↑ TX {{.Stats.TXText}}<br>↓ RX {{.Stats.RXText}}</td><td>{{.Expires.Format "02.01.2006"}}</td><td><form class="inline" method="post" action="{{$.Base}}users/qr"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><button>Показать QR</button></form><form class="inline" method="post" action="{{$.Base}}users/delete"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><button class="danger">Удалить и отключить</button></form></td></tr>{{else}}<tr><td colspan="6" class="muted">Пользователей пока нет.</td></tr>{{end}}</tbody></table></section>{{end}}<p class="muted">QUIC Lab · Echo / VPN / TLS</p></main></html>`))
