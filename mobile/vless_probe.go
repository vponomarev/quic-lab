package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"quiclab/internal/vless"
	"strings"
	"time"
)

// VLESSProbe is a bounded diagnostic handle, not a TUN backend or a subscription
// importer. Android supplies an endpoint resolved on the same Network as binder.
type VLESSProbe struct {
	ctx    context.Context
	cancel context.CancelFunc
	client vless.Client
}

func NewVLESSProbe(raw string, binder SocketBinder, budget *TrafficBudget, network string) (*VLESSProbe, error) {
	if len(raw) > 96*1024 || (network != "wifi" && network != "cell") || (network == "cell" && budget == nil) {
		return nil, errors.New("invalid VLESS probe options")
	}
	var cfg vless.Config
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&cfg); err != nil {
		return nil, errors.New("invalid VLESS probe configuration")
	}
	var extra interface{}
	if err := d.Decode(&extra); err != io.EOF {
		return nil, errors.New("invalid VLESS probe configuration")
	}
	if binder == nil {
		return nil, errors.New("VLESS requires a socket binder")
	}
	if budget != nil {
		binder = budget.Bind(binder, network)
	}
	factory, err := newVLESSSocketFactory(cfg.Endpoint, binder)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	client, err := vless.New(ctx, cfg, factory)
	if err != nil {
		cancel()
		return nil, err
	}
	return &VLESSProbe{ctx: ctx, cancel: cancel, client: client}, nil
}
func (p *VLESSProbe) Close() error { p.cancel(); return p.client.Close() }

// HTTPSStatus makes one verified HTTPS request through this profile. It returns
// only a status code; upstream error text and response bodies may contain secrets.
func (p *VLESSProbe) HTTPSStatus(target string) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, errors.New("VLESS probe is closed")
	}
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(target) > 2048 {
		return 0, errors.New("VLESS probe requires an HTTPS target")
	}
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()
	tr := &http.Transport{DialContext: p.client.DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, errors.New("invalid HTTPS probe")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, errors.New("VLESS HTTPS probe failed")
	}
	defer resp.Body.Close()
	if _, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024)); err != nil {
		return 0, errors.New("VLESS HTTPS response incomplete")
	}
	return resp.StatusCode, nil
}
