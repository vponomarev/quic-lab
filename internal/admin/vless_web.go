package admin

import (
	_ "embed"
	"html/template"
	"net/http"
	"strconv"
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
	w.page(rw, view{Title: "VLESS", Admin: true, CSRF: auth.CSRF, QR: image, ConnectionLink: raw, Enrollment: true, VLESSQR: true})
}
func (w *Web) vlessSettings(rw http.ResponseWriter, r *http.Request) {
	auth, ok := w.authorized(rw, r, false)
	if !ok {
		return
	}
	c := w.Store.vlessConfig()
	publicKey, _ := c.PublicKey()
	c.RealityPrivateKey = ""
	configured, applied, revision := w.Store.VLESSStatus()
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Referrer-Policy", "no-referrer")
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	vlessPage.Execute(rw, map[string]any{"PublicKey": publicKey, "Base": w.base, "CSRF": auth.CSRF, "Config": c, "Configured": configured, "Applied": applied, "Revision": revision, "Names": strings.Join(c.RealityServerNames, ","), "IDs": strings.Join(c.RealityShortIDs, ",")})
}
func (w *Web) vlessConfigure(rw http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(rw, r.Body, 32*1024)
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	revision, err := strconv.ParseUint(r.Form.Get("expected_revision"), 10, 64)
	if err != nil || revision == 0 {
		http.Error(rw, "Обновите страницу настроек перед сохранением.", 409)
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
		c.RealityExportShortID = ""
		c.RealitySpiderX = ""
	} else {
		c.TLSCertificateFile = ""
		c.TLSKeyFile = ""
		c.RealityTarget = strings.TrimSpace(r.Form.Get("reality_target"))
		c.RealityExportShortID = strings.TrimSpace(r.Form.Get("reality_export_short_id"))
		c.RealitySpiderX = r.Form.Get("reality_spider_x")
		c.RealityServerNames = strings.Split(r.Form.Get("reality_names"), ",")
		c.RealityShortIDs = strings.Split(r.Form.Get("reality_ids"), ",")
		for i := range c.RealityServerNames {
			c.RealityServerNames[i] = strings.TrimSpace(c.RealityServerNames[i])
		}
		for i := range c.RealityShortIDs {
			c.RealityShortIDs[i] = strings.TrimSpace(c.RealityShortIDs[i])
		}
		if key := r.Form.Get("reality_private_key"); key != "" {
			if r.Form.Get("confirm_key_change") != "yes" {
				http.Error(rw, "Подтвердите замену ключа: клиентам понадобится новый профиль.", 400)
				return
			}
			c.RealityPrivateKey = key
		}
	}
	if e := w.Store.ApplyVLESSConfig(r.Context(), c, revision); e != nil {
		http.Error(rw, e.Error(), 409)
		return
	}
	http.Redirect(rw, r, w.base+"vless", 303)
}

//go:embed vless_settings.html
var vlessHTML string
var vlessPage = template.Must(template.New("vless").Parse(vlessHTML))
