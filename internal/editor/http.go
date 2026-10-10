package editor

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"
)

// NewHandler exposes only device operations. Browser mutations require a separate
// authenticated administrator session and CSRF validation, never a device token.
func NewHandler(store *Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		fail := func(code int) { http.Error(w, http.StatusText(code), code) }
		if r.TLS == nil || r.TLS.Version < tls.VersionTLS12 {
			fail(426)
			return
		}
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			fail(405)
			return
		}
		if r.URL.RawQuery != "" {
			fail(400)
			return
		}
		op := strings.TrimPrefix(r.URL.Path, "/api/editor/v1/")
		if r.URL.Path != "/api/editor/v1/"+op || (op != "pair" && op != "poll" && op != "report" && op != "ack" && op != "revoke") {
			fail(404)
			return
		}
		media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if e != nil || media != "application/json" {
			fail(415)
			return
		}
		raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if e != nil {
			fail(413)
			return
		}
		var req struct {
			ID            string          `json:"id"`
			Inventory     json.RawMessage `json:"inventory,omitempty"`
			Generation    *int64          `json:"generation,omitempty"`
			Revision      *int64          `json:"revision,omitempty"`
			Configuration json.RawMessage `json:"configuration,omitempty"`
			Status        string          `json:"status,omitempty"`
		}
		if !strictJSON(raw, &req) || req.ID == "" {
			fail(400)
			return
		}
		if req.Inventory != nil && op != "pair" {
			fail(400)
			return
		}
		// Reject fields belonging to a different operation, including misleading ACKs.
		switch op {
		case "pair", "report":
			if req.Generation == nil || req.Configuration == nil || req.Revision != nil || req.Status != "" {
				fail(400)
				return
			}
		case "ack":
			if req.Generation == nil || req.Revision == nil || req.Configuration == nil || req.Status == "" {
				fail(400)
				return
			}
		default:
			if req.Generation != nil || req.Revision != nil || req.Configuration != nil || req.Status != "" {
				fail(400)
				return
			}
		}
		auth := r.Header.Values("Authorization")
		if len(auth) != 1 || !strings.HasPrefix(auth[0], "Bearer ") {
			fail(401)
			return
		}
		secret := strings.TrimPrefix(auth[0], "Bearer ")
		var result any = map[string]bool{"accepted": true}
		switch op {
		case "pair":
			result, e = store.Pair(req.ID, secret, *req.Generation, req.Configuration, req.Inventory)
		case "poll":
			result, e = store.Poll(req.ID, secret)
		case "report":
			e = store.Report(req.ID, secret, *req.Generation, req.Configuration)
		case "ack":
			e = store.Acknowledge(req.ID, secret, *req.Revision, *req.Generation, req.Configuration, req.Status)
		case "revoke":
			e = store.RevokeDevice(req.ID, secret)
		}
		if e != nil {
			code := 500
			switch {
			case errors.Is(e, ErrUnauthorized):
				code = 401
			case errors.Is(e, ErrInvalid):
				code = 400
			case errors.Is(e, ErrConflict):
				code = 409
			case errors.Is(e, ErrLimit):
				code = 429
			}
			fail(code)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}
func strictJSON(raw []byte, out any) bool {
	if !utf8.Valid(raw) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if !jsonValue(d, 0) {
		return false
	}
	if _, e := d.Token(); e != io.EOF {
		return false
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(out) == nil && d.Decode(new(any)) == io.EOF
}
func jsonValue(d *json.Decoder, depth int) bool {
	if depth > 16 {
		return false
	}
	t, e := d.Token()
	if e != nil || t == nil {
		return false
	}
	switch t {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			key, ok := k.(string)
			if e != nil || !ok || seen[key] {
				return false
			}
			seen[key] = true
			if !jsonValue(d, depth+1) {
				return false
			}
		}
		end, e := d.Token()
		return e == nil && end == json.Delim('}')
	case json.Delim('['):
		for d.More() {
			if !jsonValue(d, depth+1) {
				return false
			}
		}
		end, e := d.Token()
		return e == nil && end == json.Delim(']')
	}
	return true
}
