package mobile

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"quiclab/internal/vless"
	"sync"
	"time"
)

// Observe actual protected outer sockets, never a separate unprotected dial.
type probeSocketObserver struct {
	factory   vless.SocketFactory
	mu        sync.Mutex
	connected bool
	attempted bool
	last      string
}

func (o *probeSocketObserver) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	o.mu.Lock()
	o.attempted = true
	o.mu.Unlock()
	c, e := o.factory.DialContext(ctx, network, address)
	o.mu.Lock()
	defer o.mu.Unlock()
	if e == nil {
		o.connected = true
	} else {
		o.last = probeError(ctx, e)
	}
	return c, e
}
func (o *probeSocketObserver) outcome() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.connected {
		return "success"
	}
	if o.last != "" {
		return o.last
	}
	if o.attempted {
		return "pending"
	}
	return "not_attempted"
}

// This traces the inner HTTPS handshake. It does NOT claim a separate Xray
// TLS/REALITY handshake callback. Verified inner TLS proves traversal of VLESS.
type probeHTTPTrace struct {
	mu        sync.Mutex
	stage     string
	confirmed bool
}

func (t *probeHTTPTrace) set(s string) { t.mu.Lock(); t.stage = s; t.mu.Unlock() }
func (t *probeHTTPTrace) snapshot() (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stage, t.confirmed
}
func (t *probeHTTPTrace) context(ctx context.Context) context.Context {
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		TLSHandshakeStart: func() { t.set("proxy_or_target_tls") },
		TLSHandshakeDone: func(_ tls.ConnectionState, e error) {
			if e == nil {
				t.mu.Lock()
				t.confirmed = true
				t.stage = "http_response"
				t.mu.Unlock()
			}
		},
		GotFirstResponseByte: func() { t.set("http_body") },
	})
}

type probeIPResult struct {
	outcome, stage, ip, provider string
	confirmed                    bool
}

func (p *VLESSProbe) probeExitIP(parent context.Context, tc *tls.Config) probeIPResult {
	if p.exitIPURL == "" {
		return probeIPResult{outcome: "not_attempted"}
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	trace := &probeHTTPTrace{stage: "proxy_or_target_tls"}
	tr := &http.Transport{DialContext: p.client.DialContext, TLSClientConfig: tc, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 8192, DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	out := probeIPResult{provider: "ipify", outcome: "unexpected_response"}
	finish := func() probeIPResult { out.stage, out.confirmed = trace.snapshot(); return out }
	req, e := http.NewRequestWithContext(trace.context(ctx), "GET", p.exitIPURL, nil)
	if e != nil {
		return finish()
	}
	resp, e := c.Do(req)
	if e != nil {
		out.outcome = probeError(ctx, e)
		return finish()
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, 1025))
	if e != nil {
		out.outcome = probeError(ctx, e)
		return finish()
	}
	var reply struct {
		IP string `json:"ip"`
	}
	if resp.StatusCode != 200 || len(body) > 1024 || json.Unmarshal(body, &reply) != nil {
		return finish()
	}
	address, e := netip.ParseAddr(reply.IP)
	if e != nil || !address.Is4() || !address.IsGlobalUnicast() || address.IsPrivate() {
		return finish()
	}
	out.ip = address.String()
	out.outcome = "success"
	return finish()
}
