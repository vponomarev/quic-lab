package mobile

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"quiclab/internal/gateway"
	"strings"
	"time"
)

type diagnosticConn struct{ gateway.Stream }

func (s diagnosticConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (s diagnosticConn) RemoteAddr() net.Addr               { return &net.TCPAddr{Port: 443} }
func (s diagnosticConn) SetReadDeadline(t time.Time) error  { return s.SetDeadline(t) }
func (s diagnosticConn) SetWriteDeadline(t time.Time) error { return s.SetDeadline(t) }

// CheckExitIP is asynchronous and never opens a direct Internet connection on
// Android. DNS resolution occurs on the gateway; HTTPS runs inside the tunnel.
func (g *Gateway) CheckExitIP() {
	g.mu.Lock()
	if g.exitChecking || g.cancel == nil {
		g.mu.Unlock()
		return
	}
	g.exitChecking = true
	parent := g.ctx
	g.mu.Unlock()
	go func() {
		defer func() { g.mu.Lock(); g.exitChecking = false; g.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(parent, 12*time.Second)
		defer cancel()
		g.emit("exit_ip_checking", map[string]any{})
		ip, e := fetchExitIP(ctx, func(ctx context.Context) (net.Conn, error) {
			st, e := g.dialStream(ctx, "exit-ip", "")
			if e != nil {
				return nil, e
			}
			return diagnosticConn{st}, nil
		})
		if parent.Err() != nil {
			return
		}
		if e != nil {
			g.emit("exit_ip_failed", map[string]any{"detail": "РџСЂРѕРІРµСЂРєР° РІРЅРµС€РЅРµРіРѕ IPv4 РЅРµРґРѕСЃС‚СѓРїРЅР°: " + e.Error()})
			return
		}
		g.emit("exit_ip", map[string]any{"ip": ip, "provider": "api.ipify.org"})
	}()
}
func fetchExitIP(ctx context.Context, dial func(context.Context) (net.Conn, error)) (string, error) {
	tr := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) }}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("unexpected redirect") }}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://api.ipify.org/", nil)
	if e != nil {
		return "", e
	}
	response, e := client.Do(req)
	if e != nil {
		return "", e
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("IP service unavailable")
	}
	b, e := io.ReadAll(io.LimitReader(response.Body, 65))
	if e != nil {
		return "", e
	}
	return parseExitIP(string(b))
}
func parseExitIP(value string) (string, error) {
	if len(value) > 64 {
		return "", errors.New("invalid IP response")
	}
	ip, e := netip.ParseAddr(strings.TrimSpace(value))
	if e != nil || !ip.Is4() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return "", errors.New("invalid public IPv4 response")
	}
	return ip.String(), nil
}
