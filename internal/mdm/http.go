package mdm

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

func NewHandler(store *Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		if r.TLS == nil || r.TLS.Version < tls.VersionTLS12 {
			http.Error(w, "TLS 1.2 required", http.StatusUpgradeRequired)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", 405)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/mdm/v1/")
		if path != "enroll" && path != "activate" && path != "sync" && path != "pause" && path != "telemetry" {
			http.NotFound(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "request too large", 413)
			return
		}
		now := time.Now().UTC()
		respond := func(value any, e error) {
			if e != nil {
				code := 500
				switch {
				case errors.Is(e, ErrUnauthorized):
					code = 401
				case errors.Is(e, ErrConflict), errors.Is(e, ErrPaused):
					code = 409
				case errors.Is(e, ErrInvalid):
					code = 400
				case errors.Is(e, ErrLimit):
					code = 429
				}
				http.Error(w, http.StatusText(code), code)
				return
			}
			_ = json.NewEncoder(w).Encode(value)
		}
		if path == "enroll" {
			var req EnrollmentRequest
			if json.Unmarshal(raw, &req) != nil {
				respond(nil, ErrInvalid)
				return
			}
			b, e := store.Redeem(req, now)
			respond(b, e)
			return
		}
		if r.URL.Path == "/mdm/v1/telemetry" {
			var batch TelemetryBatch
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&batch) != nil || decoder.Decode(new(any)) != io.EOF {
				http.Error(w, "invalid request", 400)
				return
			}
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || !store.Authenticate(batch.BindingID, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) {
				http.Error(w, "unauthorized", 401)
				return
			}
			if e := store.AppendTelemetry(batch, time.Now().UTC()); e != nil {
				respond(nil, e)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"accepted": true})
			return
		}

		var req SyncRequest
		if json.Unmarshal(raw, &req) != nil || req.Version != 1 {
			respond(nil, ErrInvalid)
			return
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || !store.Authenticate(req.BindingID, strings.TrimPrefix(auth, "Bearer ")) {
			respond(nil, ErrUnauthorized)
			return
		}
		switch path {
		case "activate":
			b, e := store.Activate(req.BindingID, now)
			respond(b, e)
		case "pause":
			e := store.Pause(req.BindingID, req.Epoch, now)
			respond(map[string]bool{"paused": e == nil}, e)
		case "sync":
			out, e := store.WaitSync(r.Context(), req.BindingID, req, func() time.Time { return time.Now().UTC() })
			respond(out, e)
		}
	})
}
