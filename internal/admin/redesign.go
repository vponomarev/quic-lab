package admin

import (
	"crypto/tls"
	"html/template"
	"net"
	"net/http"
	"strings"
)

func (w *Web) home(rw http.ResponseWriter, r *http.Request) {
	size := w.apkSize()
	var qr template.URL
	if size != "" {
		var err error
		qr, err = qrImage(w.Config.PublicURL + "download/quic-lab.apk")
		if err != nil {
			http.Error(rw, "Download QR unavailable", 500)
			return
		}
	}
	peer := r.RemoteAddr
	version, cipher, alpn, sni := "Не определено", "Не определено", "Не согласован", "Не передано"
	if r.TLS != nil {
		version = tls.VersionName(r.TLS.Version)
		cipher = tls.CipherSuiteName(r.TLS.CipherSuite)
		if r.TLS.NegotiatedProtocol != "" {
			alpn = r.TLS.NegotiatedProtocol
		}
		if r.TLS.ServerName != "" {
			sni = r.TLS.ServerName
		}
	}
	// Only the configured local HTTP proxy may supply the original peer.
	if r.TLS == nil {
		host, _, _ := net.SplitHostPort(peer)
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			if original, err := capturePeer(r); err == nil {
				peer = original
				if value := r.Header.Get("X-Portal-TLS-Version"); value != "" {
					version = value
				}
				if value := r.Header.Get("X-Portal-TLS-Cipher"); value != "" {
					cipher = value
				}
				if value := r.Header.Get("X-Portal-TLS-ALPN"); value != "" {
					alpn = value
				}
				if value := r.Header.Get("X-Portal-TLS-SNI"); value != "" {
					sni = value
				}
			}
		}
	}
	ip, _, err := net.SplitHostPort(peer)
	if err != nil {
		ip = peer
	}
	w.page(rw, view{Title: "Добро пожаловать", Home: true, APKSize: size, APKQR: qr,
		TLSVersion: version, TLSCipher: cipher, TLSALPN: alpn, TLSSNI: sni, VisitorHTTP: r.Proto, VisitorIP: ip, VisitorAgent: r.UserAgent(), VisitorLanguage: r.Header.Get("Accept-Language")})
}

func (w *Web) userSettings(rw http.ResponseWriter, r *http.Request) {
	w.userCaptureMu.Lock()
	defer w.userCaptureMu.Unlock()
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	id := r.Form.Get("id")
	err := w.Store.setUserAccess(id, append([]string{}, r.Form["protocol"]...), nil, &name)
	// Also revoke export tickets if a durable change was only partly applied.
	w.revokeTickets(id)
	if err != nil {
		http.Error(rw, "Не удалось полностью применить настройки. Проверьте текущее состояние перед повтором. Подробности: "+err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(rw, r, w.base+"users", http.StatusSeeOther)
}
