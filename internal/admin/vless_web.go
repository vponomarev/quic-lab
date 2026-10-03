package admin

import (
	"html/template"
	"net/http"
	"strings"
)

func (w *Web) vlessQR(rw http.ResponseWriter, r *http.Request) {
	auth, ok := w.authorized(rw, r, true)
	if !ok {
		return
	}
	raw, e := w.Store.VLESSProfile(r.Form.Get("id"))
	if e != nil {
		http.Error(rw, "VLESS configuration not applied or device unavailable", 409)
		return
	}
	image, e := qrImage(raw)
	if e != nil {
		http.Error(rw, "QR unavailable", 500)
		return
	}
	w.page(rw, view{Title: "VLESS", Admin: true, CSRF: auth.CSRF, QR: image, Enrollment: true, VLESSQR: true})
}
func (w *Web) vlessSettings(rw http.ResponseWriter, r *http.Request) {
	auth, ok := w.authorized(rw, r, false)
	if !ok {
		return
	}
	c := w.Store.vlessConfig()
	c.RealityPrivateKey = ""
	configured, applied, revision := w.Store.VLESSStatus()
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	vlessPage.Execute(rw, map[string]any{"Base": w.base, "CSRF": auth.CSRF, "Config": c, "Configured": configured, "Applied": applied, "Revision": revision, "Names": strings.Join(c.RealityServerNames, ","), "IDs": strings.Join(c.RealityShortIDs, ",")})
}
func (w *Web) vlessConfigure(rw http.ResponseWriter, r *http.Request) {
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	c := w.Store.vlessConfig()
	if c.Listen == "" {
		http.Error(rw, "VLESS worker is not configured", 409)
		return
	}
	c.Listen = r.Form.Get("listen")
	c.Endpoint = r.Form.Get("endpoint")
	c.Security = r.Form.Get("security")
	c.ServerName = r.Form.Get("server_name")
	c.Fingerprint = r.Form.Get("fingerprint")
	c.Flow = r.Form.Get("flow")
	c.Mode = r.Form.Get("mode")
	c.DemuxEndpoint = r.Form.Get("demux_endpoint")
	if c.Security == "tls" {
		c.TLSCertificateFile = r.Form.Get("certificate_file")
		c.TLSKeyFile = r.Form.Get("key_file")
		c.RealityTarget = ""
		c.RealityPrivateKey = ""
		c.RealityServerNames = nil
		c.RealityShortIDs = nil
	} else {
		c.TLSCertificateFile = ""
		c.TLSKeyFile = ""
		c.RealityTarget = r.Form.Get("reality_target")
		c.RealityServerNames = strings.Split(r.Form.Get("reality_names"), ",")
		c.RealityShortIDs = strings.Split(r.Form.Get("reality_ids"), ",")
		for i := range c.RealityServerNames {
			c.RealityServerNames[i] = strings.TrimSpace(c.RealityServerNames[i])
		}
		for i := range c.RealityShortIDs {
			c.RealityShortIDs[i] = strings.TrimSpace(c.RealityShortIDs[i])
		}
		if key := r.Form.Get("reality_private_key"); key != "" {
			c.RealityPrivateKey = key
		}
	}
	if e := w.Store.UpdateVLESSConfig(c); e != nil {
		http.Error(rw, "VLESS configuration rejected or application not confirmed; check server status", 409)
		return
	}
	http.Redirect(rw, r, w.base+"vless", 303)
}

var vlessPage = template.Must(template.New("vless").Parse(`<!doctype html><html lang="ru"><meta charset="utf-8"><title>VLESS</title><style>body{font:16px system-ui;max-width:800px;margin:40px auto;padding:16px}label{display:block;margin:12px 0}input,select{display:block;min-width:350px;padding:6px}small{display:block}</style><a href="{{.Base}}users">Пользователи</a><h1>VLESS-сервер</h1>{{if .Configured}}<p>Ревизия {{.Revision}} · {{if .Applied}}Применена{{else}}Применение не подтверждено{{end}}</p><p>Изменение входа может разорвать подключения. Отзыв устройства не затрагивает соседние устройства.</p><form method="post" action="{{.Base}}vless/config"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Адрес прослушивания<input name="listen" value="{{.Config.Listen}}" required></label><label>Публичный endpoint<input name="endpoint" value="{{.Config.Endpoint}}" required></label><label>Защита<select name="security"><option value="tls" {{if eq .Config.Security "tls"}}selected{{end}}>TLS</option><option value="reality" {{if eq .Config.Security "reality"}}selected{{end}}>REALITY</option></select></label><label>SNI клиента<input name="server_name" value="{{.Config.ServerName}}" required></label><label>Fingerprint<input name="fingerprint" value="{{.Config.Fingerprint}}"></label><label>Flow<select name="flow"><option value="">Без Vision</option><option value="xtls-rprx-vision" {{if eq .Config.Flow "xtls-rprx-vision"}}selected{{end}}>Vision</option></select></label><label>Режим<select name="mode"><option value="standalone" {{if eq .Config.Mode "standalone"}}selected{{end}}>Standalone</option><option value="demux-only" {{if eq .Config.Mode "demux-only"}}selected{{end}}>Только заданный demux</option></select></label><label>Demux endpoint<input name="demux_endpoint" value="{{.Config.DemuxEndpoint}}"></label><h2>TLS</h2><label>Файл сертификата<input name="certificate_file" value="{{.Config.TLSCertificateFile}}"></label><label>Файл закрытого ключа<input name="key_file" value="{{.Config.TLSKeyFile}}"></label><h2>REALITY</h2><label>Target<input name="reality_target" value="{{.Config.RealityTarget}}"></label><label>Server names через запятую<input name="reality_names" value="{{.Names}}"></label><label>Short IDs через запятую<input name="reality_ids" value="{{.IDs}}"></label><label>Новый закрытый ключ<input type="password" name="reality_private_key" autocomplete="new-password"><small>Оставьте пустым, чтобы сохранить текущий ключ.</small></label><button>Сохранить и применить</button></form>{{else}}<p>VLESS worker не настроен в установке сервера.</p>{{end}}</html>`))
