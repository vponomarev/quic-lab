package mobile

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"quiclab/internal/trafficbudget"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// MDMResolver resolves on the same physical network as the supplied socket binder.
type MDMResolver interface {
	ResolveAddress(host string, port int) (string, error)
}

// MDMChannel owns a cancellable control connection, independently of VPN lifetime.
// One instance belongs to one physical network generation. Close permanently retires it.
type MDMChannel struct {
	budget    *TrafficBudget
	resolver  MDMResolver
	tlsConfig *tls.Config
	mu        sync.Mutex
	closed    bool
	cancel    context.CancelFunc
}

func NewMDMChannel(budget *TrafficBudget, resolver MDMResolver, ca string) (*MDMChannel, error) {
	if budget == nil || resolver == nil {
		return nil, errors.New("MDM requires budget and physical resolver")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if ca != "" {
		config.RootCAs = x509.NewCertPool()
		if !config.RootCAs.AppendCertsFromPEM([]byte(ca)) {
			return nil, errors.New("invalid MDM CA")
		}
	}
	return &MDMChannel{budget: budget, resolver: resolver, tlsConfig: config}, nil
}
func (c *MDMChannel) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.cancel != nil {
		c.cancel()
	}
}

// Exchange never follows redirects, persists credentials, or exposes server bodies in errors.
func (c *MDMChannel) Exchange(endpoint, secret, body string, binder SocketBinder) (string, error) {
	u, e := serviceURL(endpoint)
	if e != nil {
		return "", e
	}
	if u.Path != "/mdm/v1/enroll" && u.Path != "/mdm/v1/activate" && u.Path != "/mdm/v1/sync" && u.Path != "/mdm/v1/pause" {
		return "", errors.New("invalid MDM operation")
	}
	if len(secret) > 4096 || strings.ContainsAny(secret, "\r\n") || len(body) > 1<<20 || !json.Valid([]byte(body)) {
		return "", errors.New("invalid MDM request")
	}
	physical, ok := binder.(*budgetBinder)
	if !ok || physical.budget != c.budget || (physical.network != "wifi" && physical.network != "cell") {
		return "", errors.New("MDM requires physical budget binding")
	}
	allowed := func() error {
		if physical.network == "cell" && !c.budget.CellAllowed() {
			return trafficbudget.ErrBlocked
		}
		return nil
	}
	if e = allowed(); e != nil {
		return "", e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	c.mu.Lock()
	if c.closed || c.cancel != nil {
		c.mu.Unlock()
		cancel()
		return "", errors.New("MDM channel closed or busy")
	}
	c.cancel = cancel
	c.mu.Unlock()
	defer func() { cancel(); c.mu.Lock(); c.cancel = nil; c.mu.Unlock() }()
	dialer := net.Dialer{Timeout: 10 * time.Second, Control: func(_, _ string, raw syscall.RawConn) error {
		var err error
		e := raw.Control(func(fd uintptr) { err = physical.Bind(int64(fd)) })
		if e != nil {
			return e
		}
		return err
	}}
	transport := &http.Transport{TLSClientConfig: c.tlsConfig.Clone(), DisableKeepAlives: true, DisableCompression: true, ResponseHeaderTimeout: 30 * time.Second,
		DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
			if e := ctx.Err(); e != nil {
				return nil, e
			}
			if e := allowed(); e != nil {
				return nil, e
			}
			host, port, e := net.SplitHostPort(address)
			if e != nil {
				return nil, e
			}
			number, e := strconv.Atoi(port)
			if e != nil {
				return nil, e
			}
			numeric, e := c.resolver.ResolveAddress(host, number)
			if e != nil {
				return nil, errors.New("MDM physical DNS unavailable")
			}
			ip, p, e := net.SplitHostPort(numeric)
			if e != nil || net.ParseIP(ip) == nil || p != port {
				return nil, errors.New("invalid MDM resolved address")
			}
			if e = ctx.Err(); e != nil {
				return nil, e
			}
			if e = allowed(); e != nil {
				return nil, e
			}
			conn, e := dialer.DialContext(ctx, "tcp", numeric)
			if e != nil {
				return nil, e
			}
			return meterConn(conn, physical.network, c.budget, trafficbudget.User), nil
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(body))
	if e != nil {
		return "", errors.New("invalid MDM request")
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	response, e := client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if e = allowed(); e != nil {
			return "", e
		}
		return "", errors.New("MDM HTTPS exchange failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("MDM HTTP status " + strconv.Itoa(response.StatusCode))
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if e != nil {
		return "", errors.New("MDM response interrupted")
	}
	if len(raw) > 1<<20 || !json.Valid(raw) {
		return "", errors.New("invalid MDM response")
	}
	return string(raw), nil
}
