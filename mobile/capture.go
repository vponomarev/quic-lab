package mobile

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

type captureConfig struct {
	Kind  string `json:"capture_kind"`
	Host  string `json:"hostname"`
	URL   string `json:"capture_url"`
	Token string `json:"capture_token"`
	Until int64  `json:"capture_until"`
}
type captureUpload struct {
	cfg                     captureConfig
	ctx                     context.Context
	cancel                  context.CancelFunc
	queue                   chan []byte
	mu                      sync.Mutex
	sent, failures, dropped int
}

var captureState struct {
	sync.Mutex
	active *captureUpload
}

func parseCapture(raw string) (captureConfig, error) {
	var c captureConfig
	if json.Unmarshal([]byte(raw), &c) != nil {
		return c, errors.New("invalid debug QR")
	}
	u, e := url.Parse(c.URL)
	valid := regexp.MustCompile(`^[a-f0-9]{64}$`)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || !strings.HasSuffix(u.Path, "/capture/keys") || !valid.MatchString(u.Query().Get("id")) || !valid.MatchString(c.Token) || c.Host == "" || (c.Kind != "echo" && c.Kind != "vpn") || c.Until <= time.Now().Unix() || c.Until > time.Now().Add(11*time.Minute).Unix() {
		return c, errors.New("invalid or expired debug capture QR")
	}
	return c, nil
}
func ValidateDebugCapture(raw string) error { _, e := parseCapture(raw); return e }
func ConfigureDebugCapture(raw string) error {
	c, e := parseCapture(raw)
	if e != nil {
		return e
	}
	StopDebugCapture()
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(c.Until, 0))
	up := &captureUpload{cfg: c, ctx: ctx, cancel: cancel, queue: make(chan []byte, 64)}
	captureState.Lock()
	captureState.active = up
	captureState.Unlock()
	go up.run()
	return nil
}
func StopDebugCapture() {
	captureState.Lock()
	defer captureState.Unlock()
	if captureState.active != nil {
		captureState.active.cancel()
		captureState.active = nil
	}
}
func DebugCaptureStatus() string {
	captureState.Lock()
	up := captureState.active
	captureState.Unlock()
	if up == nil || up.ctx.Err() != nil {
		return ""
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	return fmt.Sprintf("Wireshark · %s · ключей отправлено %d · ошибок %d · пропусков %d", up.cfg.Kind, up.sent, up.failures, up.dropped)
}
func debugKeyLog(kind, host string) io.Writer {
	captureState.Lock()
	defer captureState.Unlock()
	up := captureState.active
	if up == nil || up.ctx.Err() != nil || up.cfg.Kind != kind || !strings.EqualFold(up.cfg.Host, host) {
		return nil
	}
	return up
}
func (c *captureUpload) Write(p []byte) (int, error) {
	if c.ctx.Err() == nil {
		copyOf := append([]byte(nil), p...)
		select {
		case c.queue <- copyOf:
		default:
			c.mu.Lock()
			c.dropped++
			c.mu.Unlock()
		}
	}
	return len(p), nil
}
func (c *captureUpload) run() {
	// This transport intentionally has no KeyLogWriter, preventing recursion.
	tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 5 * time.Second}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("debug upload redirects forbidden") }}
	for {
		var batch []byte
		select {
		case <-c.ctx.Done():
			return
		case batch = <-c.queue:
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-c.ctx.Done():
			return
		}
		for len(batch) < 6000 && len(c.queue) > 0 {
			batch = append(batch, <-c.queue...)
		}
		success := false
		for attempt := 0; attempt < 2; attempt++ {
			req, _ := http.NewRequestWithContext(c.ctx, "POST", c.cfg.URL, bytes.NewReader(batch))
			req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
			req.Header.Set("Content-Type", "text/plain")
			resp, e := client.Do(req)
			if e == nil {
				io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				resp.Body.Close()
				if resp.StatusCode == 204 {
					success = true
					break
				}
				if resp.StatusCode == 403 {
					c.cancel()
					break
				}
			}
			if c.ctx.Err() != nil {
				return
			}
		}
		c.mu.Lock()
		if success {
			c.sent += bytes.Count(batch, []byte{'\n'})
		} else {
			c.failures++
		}
		c.mu.Unlock()
	}
}
