// Package probeecho provides a bounded, destination-free HTTPS reachability target.
package probeecho

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

func NewHandler() http.Handler {
	var mu sync.Mutex
	tokens := 100.0
	last := time.Now()
	slots := make(chan struct{}, 16)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		mu.Lock()
		now := time.Now()
		tokens = min(100, tokens+now.Sub(last).Seconds()*100)
		last = now
		allowed := tokens >= 1
		if allowed {
			tokens--
		}
		mu.Unlock()
		if !allowed {
			http.Error(w, "rate limited", 429)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "busy", 429)
			return
		}
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method", 405)
			return
		}
		// Bound body read time as well as bytes; anonymous slow writers cannot hold all slots.
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(5 * time.Second))
		defer http.NewResponseController(w).SetReadDeadline(time.Time{})
		var v struct {
			Version int    `json:"version"`
			Nonce   string `json:"nonce"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		d.DisallowUnknownFields()
		err := d.Decode(&v)
		var extra any
		if err != nil || d.Decode(&extra) != io.EOF || v.Version != 1 || len(v.Nonce) != 64 {
			http.Error(w, "invalid probe", 400)
			return
		}
		if _, err = hex.DecodeString(v.Nonce); err != nil {
			http.Error(w, "invalid probe", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	})
}
