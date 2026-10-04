package mobile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"quiclab/internal/vless"
	"strings"
	"time"
)

// Stored profiles must be the canonical bridge output, never arbitrary Xray JSON.
func decodeGatewayVLESS(raw, endpoint string) (vless.Config, error) {
	fail := func() (vless.Config, error) { return vless.Config{}, errors.New("invalid VLESS stored profile") }
	if len(raw) == 0 || len(raw) > 16*1024 {
		return fail()
	}
	var p vless.ImportedProfile
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&p) != nil {
		return fail()
	}
	var extra any
	if dec.Decode(&extra) != io.EOF || len(p.Config.RootPEM) != 0 {
		return fail()
	}
	canonical, err := json.Marshal(p)
	if err != nil || !bytes.Equal(canonical, []byte(raw)) {
		return fail()
	}
	q := url.Values{"security": {p.Config.Security}, "sni": {p.Config.ServerName}, "encryption": {"none"}, "type": {"tcp"}}
	if p.Config.Flow != "" {
		q.Set("flow", p.Config.Flow)
	}
	if p.Config.Fingerprint != "" {
		q.Set("fp", p.Config.Fingerprint)
	}
	if p.Config.RealityPublicKey != "" {
		q.Set("pbk", p.Config.RealityPublicKey)
	}
	if p.Config.ShortID != "" {
		q.Set("sid", p.Config.ShortID)
	}
	if p.Config.SpiderX != "" {
		q.Set("spx", p.Config.SpiderX)
	}
	u := url.URL{Scheme: "vless", User: url.User(p.Config.UUID), Host: p.Config.Endpoint, RawQuery: q.Encode(), Fragment: p.Name}
	checked, err := vless.ParseImport(u.String())
	if err != nil {
		return fail()
	}
	checked.Config.Endpoint = endpoint
	return checked.Config, nil
}

// Called with g.mu held. Startup creates an engine, but health is established
// only by a verified HTTPS response through that engine.
func (g *Gateway) startVLESSLocked(binder SocketBinder) error {
	c, err := decodeGatewayVLESS(g.cfg.VLESSConfig, g.cfg.Endpoint)
	if err != nil {
		return err
	}
	f, err := newVLESSSocketFactory(g.cfg.Endpoint, binder)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	engine, err := vless.New(ctx, c, f)
	if err != nil {
		cancel()
		return errors.New("VLESS engine configuration rejected")
	}
	g.ctx, g.cancel, g.vless = ctx, cancel, engine
	g.session++
	id := fmt.Sprintf("vless-%d", g.session)
	g.emit("connected", map[string]any{"transport": "vless", "session": g.session, "detail": "VLESS engine ready; waiting for verified tunnel probe"})
	go g.vlessHeartbeat(ctx, engine, id)
	go g.vlessTCPStatus(ctx, engine, f)
	return nil
}

// SetEndpoint accepts Android's resolution on the newly selected physical
// Network. Reconnect/MigrateTo applies it to a fresh engine, preserving SNI.
func (g *Gateway) SetEndpoint(endpoint string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cfg.Transport != "vless" {
		return errors.New("endpoint update requires VLESS")
	}
	if _, err := newVLESSSocketFactory(endpoint, vlessEndpointValidator{}); err != nil {
		return err
	}
	g.cfg.Endpoint = endpoint
	return nil
}

type vlessEndpointValidator struct{}

func (vlessEndpointValidator) Bind(int64) error {
	return errors.New("validation binder cannot open sockets")
}

func (g *Gateway) closeVLESSLocked() {
	if g.cancel != nil {
		g.cancel()
		g.cancel = nil
	}
	if g.vless != nil {
		g.vless.Close()
		g.vless = nil
	}
}

func (g *Gateway) vlessHeartbeat(ctx context.Context, d vless.Client, id string) {
	for ctx.Err() == nil {
		started := time.Now()
		probe, cancel := context.WithTimeout(ctx, 12*time.Second)
		ip, err := fetchExitIP(probe, func(call context.Context) (net.Conn, error) { return d.DialContext(call, "tcp4", "api.ipify.org:443") })
		cancel()
		g.mu.Lock()
		if ctx.Err() != nil || g.vless != d {
			g.mu.Unlock()
			return
		}
		if err == nil {
			g.emit("echo", map[string]any{"rtt_ms": float64(time.Since(started)) / float64(time.Millisecond), "connection_id": id, "probe": "https"})
			g.emit("exit_ip", map[string]any{"ip": ip, "provider": "api.ipify.org"})
		} else {
			g.emit("probe_unavailable", map[string]any{"detail": "VLESS verified HTTPS tunnel probe unavailable"})
		}
		g.mu.Unlock()
		if !g.waitVLESSProbe(ctx, started) {
			return
		}
	}
}
func (g *Gateway) waitVLESSProbe(ctx context.Context, started time.Time) bool {
	for {
		_, delay, changed := g.probes.snapshot(5 * time.Second)
		delay = max(delay, 5*time.Second)
		timer := time.NewTimer(max(time.Duration(0), delay-time.Since(started)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-changed:
			timer.Stop()
			started = time.Now()
		case <-timer.C:
			return ctx.Err() == nil
		}
	}
}

func (g *Gateway) vlessTCPStatus(ctx context.Context, engine vless.Client, factory *vlessSocketFactory) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		g.mu.Lock()
		if ctx.Err() != nil || g.vless != engine {
			g.mu.Unlock()
			return
		}
		g.emit("vless_tcp", map[string]any{"tcp_state": factory.connectionState()})
		g.mu.Unlock()
	}
}
