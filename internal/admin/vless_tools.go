package admin

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var vlessTargetSlots = make(chan struct{}, 2)

func (w *Web) vlessTool(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Referrer-Policy", "no-referrer")
	r.Body = http.MaxBytesReader(rw, r.Body, 16*1024)
	if _, ok := w.authorized(rw, r, true); !ok {
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "/generate-key"):
		key, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			http.Error(rw, "Key generation failed", 500)
			return
		}
		json.NewEncoder(rw).Encode(map[string]string{"private_key": base64.RawURLEncoding.EncodeToString(key.Bytes()), "public_key": base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())})
	case strings.HasSuffix(r.URL.Path, "/generate-short-id"):
		var id [8]byte
		if _, err := rand.Read(id[:]); err != nil {
			http.Error(rw, "ID generation failed", 500)
			return
		}
		json.NewEncoder(rw).Encode(map[string]string{"short_id": hex.EncodeToString(id[:])})
	case strings.HasSuffix(r.URL.Path, "/check-target"):
		target, name := strings.TrimSpace(r.Form.Get("target")), strings.TrimSpace(r.Form.Get("server_name"))
		host, port, err := net.SplitHostPort(target)
		p, e := strconv.Atoi(port)
		if err != nil || e != nil || p < 1 || p > 65535 || host == "" || name == "" || len(name) > 253 || strings.ContainsAny(host+name, " /\\\r\n\t@?#") {
			http.Error(rw, "Invalid target or SNI", 400)
			return
		}
		select {
		case vlessTargetSlots <- struct{}{}:
			defer func() { <-vlessTargetSlots }()
		default:
			http.Error(rw, "Target checks busy", 429)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		raw, err := (&net.Dialer{}).DialContext(ctx, "tcp4", target)
		result := map[string]any{"reachable": err == nil, "tls13": false, "certificate_valid": false}
		if err == nil {
			defer raw.Close()
			conn := tls.Client(raw, &tls.Config{ServerName: name, MinVersion: tls.VersionTLS13})
			if err = conn.HandshakeContext(ctx); err == nil {
				result["tls13"] = true
				result["certificate_valid"] = true
				result["message"] = "TLS 1.3 и сертификат SNI проверены. Это не гарантия доступности VPN."
			} else {
				result["message"] = "TCP доступен, проверка TLS 1.3 / сертификата не прошла."
			}
		} else {
			result["message"] = "Не удалось установить TCP-соединение за отведённое время."
		}
		json.NewEncoder(rw).Encode(result)
	default:
		http.NotFound(rw, r)
	}
}
