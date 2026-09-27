package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"quiclab/internal/debugcapture"
)

func (w *Web) captureRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /download/capture/{platform}", w.captureHelperDownload)
	m.HandleFunc("POST /capture/keys", w.captureKeys)
	m.HandleFunc("GET /capture", w.capturePage)
	m.HandleFunc("POST /capture/start", w.captureStart)
	m.HandleFunc("POST /capture/stop", w.captureStop)
	m.HandleFunc("POST /capture/launch", w.captureLaunch)
	m.HandleFunc("POST /capture/qr", w.captureQR)
	m.HandleFunc("GET /capture/download", w.captureDownload)
	m.HandleFunc("POST /capture/redeem", w.captureRedeem)
	m.HandleFunc("GET /capture/stream", w.captureStream)
}

type captureView struct {
	Base, CSRF, Message, Host string
	Enabled                   bool
	Sessions                  []debugcapture.Info
	Users                     []User
	Launch, QR                template.URL
}

func (w *Web) renderCapture(rw http.ResponseWriter, s session, message string, link, img template.URL) {
	v := captureView{Base: w.base, CSRF: s.CSRF, Message: message, Host: w.Config.Echo.Hostname, Enabled: w.Capture != nil, Launch: link, QR: img}
	if w.Capture != nil {
		v.Sessions = w.Capture.List(s.CSRF)
		sort.Slice(v.Sessions, func(i, j int) bool { return v.Sessions[i].Until.After(v.Sessions[j].Until) })
		v.Users = w.Store.List()
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = captureTemplate.Execute(rw, v)
}
func (w *Web) capturePage(rw http.ResponseWriter, r *http.Request) {
	s, ok := w.authorized(rw, r, false)
	if ok {
		w.renderCapture(rw, s, "", "", "")
	}
}
func (w *Web) captureStart(rw http.ResponseWriter, r *http.Request) {
	s, ok := w.authorized(rw, r, true)
	if !ok {
		return
	}
	if w.Capture == nil {
		http.Error(rw, "Capture disabled in server config", 503)
		return
	}
	user := ""
	kind := r.Form.Get("kind")
	_, e := w.Capture.Start(s.CSRF, kind, user)
	if e != nil {
		w.renderCapture(rw, s, e.Error(), "", "")
		return
	}
	http.Redirect(rw, r, w.base+"capture", 303)
}
func (w *Web) ownedCapture(rw http.ResponseWriter, r *http.Request, post bool) (session, *debugcapture.Session) {
	ss, ok := w.authorized(rw, r, post)
	if !ok {
		return ss, nil
	}
	if w.Capture == nil {
		http.NotFound(rw, r)
		return ss, nil
	}
	id := r.URL.Query().Get("id")
	if post {
		id = r.Form.Get("id")
	}
	s := w.Capture.Get(id, ss.CSRF)
	if s == nil {
		http.NotFound(rw, r)
	}
	return ss, s
}
func (w *Web) captureStop(rw http.ResponseWriter, r *http.Request) {
	_, s := w.ownedCapture(rw, r, true)
	if s == nil {
		return
	}
	s.Stop("stopped")
	http.Redirect(rw, r, w.base+"capture", 303)
}
func (w *Web) captureLaunch(rw http.ResponseWriter, r *http.Request) {
	ss, s := w.ownedCapture(rw, r, true)
	if s == nil {
		return
	}
	ticket, e := s.Ticket()
	if e != nil {
		w.renderCapture(rw, ss, e.Error(), "", "")
		return
	}
	u := url.URL{Scheme: "quic-lab", Host: "capture", RawQuery: url.Values{"server": {w.Config.PublicURL}, "id": {s.ID}}.Encode(), Fragment: ticket}
	// Only a locally constructed, fixed-scheme URL is marked safe for html/template.
	w.renderCapture(rw, ss, "Ссылка действует одну минуту. Откройте Wireshark, затем запустите новое соединение с отладочным портом.", template.URL(u.String()), "")
}
func (w *Web) captureQR(rw http.ResponseWriter, r *http.Request) {
	ss, s := w.ownedCapture(rw, r, true)
	if s == nil {
		return
	}
	if s.Snapshot().State != "running" {
		http.Error(rw, "Capture stopped", 410)
		return
	}
	host := w.Config.Echo.Hostname
	if s.Kind == "vpn" {
		host = w.Config.VPN.Hostname
	}
	// Only an upload capability, never a VPN identity or capture read token.
	p := map[string]any{"version": 1, "kind": "capture", "hostname": host, "capture_kind": s.Kind, "capture_url": w.Config.PublicURL + "capture/keys?id=" + s.ID, "capture_token": s.UploadToken(), "capture_until": s.Until.Unix()}
	raw, _ := json.Marshal(p)
	img, e := qrImage(string(raw))
	if e != nil {
		http.Error(rw, "QR unavailable", 500)
		return
	}
	w.renderCapture(rw, ss, "QR включает учебный экспорт TLS secrets на телефоне на 10 минут. Адреса подключения и VPN-профили не меняются.", "", img)
}
func (w *Web) captureDownload(rw http.ResponseWriter, r *http.Request) {
	_, s := w.ownedCapture(rw, r, false)
	if s == nil {
		return
	}
	rw.Header().Set("Content-Type", "application/octet-stream")
	rw.Header().Set("Content-Disposition", `attachment; filename="quic-lab-debug.pcapng"`)
	blocks, _ := s.Blocks(0)
	if len(blocks) > 0 {
		_, _ = rw.Write(blocks[0])
		_, _ = rw.Write(s.SecretBlock())
		blocks = blocks[1:]
	}
	for _, b := range blocks {
		if _, e := rw.Write(b); e != nil {
			return
		}
	}
}
func (w *Web) captureRedeem(rw http.ResponseWriter, r *http.Request) {
	if w.Capture == nil {
		http.NotFound(rw, r)
		return
	}
	var b struct{ ID, Ticket string }
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		http.Error(rw, "Invalid request", 400)
		return
	}
	token, e := w.Capture.Redeem(b.ID, b.Ticket)
	if e != nil {
		http.Error(rw, "Expired ticket", 410)
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(map[string]any{"token": token, "port": w.Capture.Port(b.ID), "tcp_port": w.Capture.TCPPort(b.ID)})
}
func (w *Web) captureStream(rw http.ResponseWriter, r *http.Request) {
	if w.Capture == nil {
		http.NotFound(rw, r)
		return
	}
	if r.Header.Get("Origin") != "" {
		http.Error(rw, "Native helper required", 403)
		return
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		http.Error(rw, "Token required", 401)
		return
	}
	s := w.Capture.Attach(r.URL.Query().Get("id"), token)
	if s == nil {
		http.Error(rw, "Capture unavailable", 403)
		return
	}
	peer, e := capturePeer(r)
	if e != nil {
		s.Stop("proxy peer unavailable")
		http.Error(rw, e.Error(), 503)
		return
	}
	release := w.Capture.Exclude(peer)
	defer release()
	c, e := websocket.Accept(rw, r, nil)
	if e != nil {
		s.Stop("attach failed")
		return
	}
	defer c.CloseNow()
	defer s.Stop("viewer disconnected")
	ctx := c.CloseRead(r.Context())
	index := 0
	keyCount := 0
	lastPing := time.Now()
	// Section header must precede DSBs. Packets are delayed two seconds so
	// asynchronous Android uploads normally arrive before their encrypted records.
	blocks, _ := s.Blocks(0)
	if len(blocks) == 0 {
		return
	}
	if c.Write(ctx, websocket.MessageBinary, blocks[0]) != nil {
		return
	}
	index = 1
	for {
		keys := s.Keys()
		if len(keys) > keyCount {
			if c.Write(ctx, websocket.MessageBinary, s.SecretBlockSince(keyCount)) != nil {
				return
			}
			keyCount = len(keys)
		}
		blocks, done := s.LiveBlocks(index)
		for _, b := range blocks {
			write, cancel := context.WithTimeout(ctx, 10*time.Second)
			e = c.Write(write, websocket.MessageBinary, b)
			cancel()
			if e != nil {
				return
			}
			index++
		}
		if done {
			_ = c.Close(websocket.StatusNormalClosure, s.Snapshot().State)
			return
		}
		if len(blocks) == 0 && time.Since(lastPing) > 15*time.Second {
			lastPing = time.Now()
			ping, cancel := context.WithTimeout(ctx, 5*time.Second)
			e = c.Ping(ping)
			cancel()
			if e != nil {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

var captureTemplate = template.Must(template.New("capture").Parse(`<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Wireshark · QUIC Lab</title><style>
body{font:16px system-ui;margin:0;background:#eff5f7;color:#173248}main{max-width:1700px;margin:auto;padding:28px}nav,.actions,form{display:flex;align-items:center;gap:12px;flex-wrap:wrap}nav{justify-content:space-between}a{color:#00877e}h1{font-size:28px}.card{background:white;border-radius:18px;padding:22px;margin:18px 0}.muted{color:#62798c}button,.launch{border:0;border-radius:8px;padding:11px 16px;background:#00877e;color:white;text-decoration:none;cursor:pointer}select{padding:10px;border:1px solid #bdcfd9;border-radius:7px;max-width:100%}table{border-collapse:collapse;width:100%}td,th{text-align:left;padding:14px 8px;border-bottom:1px solid #e6eef1}code{overflow-wrap:anywhere}.table{overflow:auto}.qr img{max-width:100%;width:320px}.alert{border-left:4px solid #00877e;padding:12px}.danger{background:#a33a40}small{display:block}.actions form{display:inline-flex}li{margin:8px 0}
</style><main><nav><a href="{{.Base}}users">← Пользователи QUIC Lab</a><a href="{{.Base}}capture">Обновить статус</a></nav><h1>Wireshark · учебная отладка</h1>{{if .Message}}<p class="alert">{{.Message}}</p>{{end}}{{if .Launch}}<section class="card"><h2>Без установки</h2><p>Скопируйте одноразовую ссылку (действует минуту). Распакуйте ZIP и передайте её программе параметром <code>-connect</code>.</p><textarea readonly aria-label="Одноразовая ссылка" style="width:100%;height:90px;box-sizing:border-box">{{.Launch}}</textarea><p><code>.\quic-lab-capture-cli.exe -connect "ССЫЛКА" -wireshark "ПУТЬ К WIRESHARK.EXE"</code></p><p class="muted">Для macOS: ./quic-lab-capture-darwin-arm64 -connect "ССЫЛКА" (Intel: amd64). Установщик и регистрация схемы не нужны.</p><details><summary>Если обработчик ссылки уже установлен</summary><p><a class="launch" href="{{.Launch}}">Открыть в Wireshark</a></p></details></section>{{end}}{{if .QR}}<section class="card qr"><img src="{{.QR}}" alt="QR отладочного Echo-профиля"></section>{{end}}
<section class="card"><ol><li>Подготовьте Wireshark и распакуйте QUIC Lab Capture для Windows или macOS: <a href="{{.Base}}download/capture/windows">Windows ZIP</a> · <a href="{{.Base}}download/capture/darwin">macOS ZIP</a>. Можно запускать из командной строки без установщика.</li><li>Создайте сессию и нажмите «Подготовить запуск», затем скопируйте одноразовую ссылку в команду.</li><li>Импортируйте QR отладки на обновлённый Android-клиент и подтвердите экспорт TLS secrets. Запустите новое Echo/VPN-соединение на обычных портах.</li><li>Переключайте Wi-Fi/LTE. После занятия остановите захват; запись уже сохранена на компьютере.</li></ol><p class="muted">Существующие порты, 10 минут / 32 MiB. Запись содержит TLS secrets и позволяет читать учебный трафик. AWG не расшифровывается этим способом. На выбранных портах записываются и другие зашифрованные подключения; ключи передаёт только телефон, включивший отладку. Сам поток Wireshark исключён из захвата. Сервер хранит запись до 5 минут после срока сессии. Поток отстаёт примерно на 2 секунды, чтобы получить секреты с телефона. Если ключи пришли поздно, скачайте и откройте завершённую запись.</p></section>
{{if .Enabled}}<section class="card"><form method="post" action="{{.Base}}capture/start"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Режим <select name="kind"><option value="echo">Echo · QUIC + HTTPS</option><option value="vpn">VPN · QUIC + HTTPS</option></select></label><button>Начать отладочную сессию</button></form></section><section class="card table"><table><thead><tr><th>Сессия</th><th>UDP / TCP</th><th>Статус</th><th>До (UTC)</th><th>Действия</th></tr></thead><tbody>{{range .Sessions}}<tr><td>{{.Kind}}<small>{{printf "%.8s" .ID}}</small></td><td>{{.Port}} / {{.TCPPort}}</td><td>{{.State}}<small>{{.Bytes}} байт</small></td><td>{{.Until.UTC.Format "15:04:05"}}</td><td class="actions">{{if eq .State "running"}}<form method="post" action="{{$.Base}}capture/launch"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><button>Подготовить запуск</button></form><form method="post" action="{{$.Base}}capture/qr"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><button>QR отладки</button></form><form method="post" action="{{$.Base}}capture/stop"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><button class="danger">Остановить</button></form>{{end}}<a href="{{$.Base}}capture/download?id={{.ID}}">Скачать pcapng</a></td></tr>{{else}}<tr><td colspan="5">Ваших сессий пока нет.</td></tr>{{end}}</tbody></table></section>{{else}}<section class="card">Отладка выключена в конфиге сервера. Инструкция: <code>docs/wireshark-capture.md</code>.</section>{{end}}</main></html>`))

func (w *Web) captureHelperDownload(rw http.ResponseWriter, r *http.Request) {
	platform := r.PathValue("platform")
	if platform != "windows" && platform != "darwin" {
		http.NotFound(rw, r)
		return
	}
	name := "quic-lab-capture-" + platform + ".zip"
	rw.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(rw, r, filepath.Join(w.Config.DataDir, "downloads", "capture", name))
}

func (w *Web) captureKeys(rw http.ResponseWriter, r *http.Request) {
	if w.Capture == nil {
		http.NotFound(rw, r)
		return
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	body, e := io.ReadAll(io.LimitReader(r.Body, 8193))
	if !ok || e != nil || !w.Capture.Upload(r.URL.Query().Get("id"), token, body) {
		http.Error(rw, "Capture upload rejected", 403)
		return
	}
	rw.WriteHeader(204)
}
func capturePeer(r *http.Request) (string, error) {
	peer := r.RemoteAddr
	if r.TLS == nil {
		host, _, e := net.SplitHostPort(peer)
		ip := net.ParseIP(host)
		if e != nil || ip == nil || !ip.IsLoopback() {
			return "", fmt.Errorf("capture admin requires HTTPS or a loopback proxy")
		}
		peer = r.Header.Get("X-Quic-Lab-Peer")
	}
	host, port, e := net.SplitHostPort(peer)
	if e != nil || net.ParseIP(host) == nil {
		return "", fmt.Errorf("set nginx proxy_set_header X-Quic-Lab-Peer $remote_addr:$remote_port for the admin location")
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("invalid proxy peer")
	}
	return net.JoinHostPort(host, port), nil
}
