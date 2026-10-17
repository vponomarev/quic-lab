package mobile

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"
)

type probeRunResult struct {
	Outcome       string `json:"outcome"`
	TotalMS       int64  `json:"totalMs"`
	EchoRTTMS     *int64 `json:"echoRttMs,omitempty"`
	EchoSucceeded int    `json:"echoSucceeded"`
	RelayOutcome  string `json:"relayOutcome"`
}
type probeRunOptions struct {
	CA      string          `json:"ca,omitempty"`
	Target  string          `json:"target"`
	Gateway json.RawMessage `json:"gateway,omitempty"`
}

func (p *VLESSProbe) Run(raw string) (string, error) { return p.run(raw, nil) }
func (p *VLESSProbe) run(raw string, tc *tls.Config) (string, error) {
	if len(raw) > 128*1024 {
		return "", errors.New("probe options too large")
	}
	var o probeRunOptions
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&o) != nil || d.Decode(&extra) != io.EOF {
		return "", errors.New("invalid probe options")
	}
	u, e := url.Parse(o.Target)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(o.Target) > 2048 {
		return "", errors.New("HTTPS target required")
	}
	if o.CA != "" {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(o.CA)) {
			return "", errors.New("invalid probe CA")
		}
		tc = &tls.Config{RootCAs: roots}
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(p.ctx, 20*time.Second)
	defer cancel()
	result := probeRunResult{Outcome: "success", RelayOutcome: "not_applicable"}
	finish := func() (string, error) {
		result.TotalMS = time.Since(started).Milliseconds()
		b, _ := json.Marshal(result)
		return string(b), nil
	}
	tr := &http.Transport{DialContext: p.client.DialContext, TLSClientConfig: tc, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 8192}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	times := []int64{}
	// The first exchange establishes the connection; RTT samples use the warmed connection.
	for i := 0; i < 4; i++ {
		nonce := make([]byte, 32)
		if _, e = rand.Read(nonce); e != nil {
			return "", errors.New("random source unavailable")
		}
		n := hex.EncodeToString(nonce)
		body, _ := json.Marshal(map[string]any{"version": 1, "nonce": n})
		req, _ := http.NewRequestWithContext(ctx, "POST", o.Target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			result.Outcome = probeError(ctx, err)
			return finish()
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 1025))
		resp.Body.Close()
		var reply struct {
			Version int    `json:"version"`
			Nonce   string `json:"nonce"`
		}
		if err != nil || len(b) > 1024 || resp.StatusCode != 200 || json.Unmarshal(b, &reply) != nil || reply.Version != 1 || reply.Nonce != n {
			result.Outcome = "unexpected_response"
			return finish()
		}
		if i > 0 {
			times = append(times, time.Since(start).Milliseconds())
			result.EchoSucceeded++
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	median := times[len(times)/2]
	result.EchoRTTMS = &median
	if len(o.Gateway) > 0 && string(o.Gateway) != "null" {
		result.RelayOutcome = p.probeRelay(ctx, o.Gateway)
		if result.RelayOutcome != "success" {
			result.Outcome = "relay_failed"
		}
	}
	return finish()
}
func probeError(ctx context.Context, e error) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "canceled"
	}
	if ctx.Err() != nil {
		return "timeout"
	}
	var cert *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(e, &cert) || errors.As(e, &unknown) || errors.As(e, &hostname) || errors.As(e, &invalid) {
		return "target_tls"
	}
	var timeout interface{ Timeout() bool }
	if errors.As(e, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	return "transport"
}
