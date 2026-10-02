package mobile

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"quiclab/internal/protocol"
	"syscall"
	"time"
)

func (c gatewayConfig) tlsNames() (string, string, error) {
	sni, verify := c.ServerName, c.VerifyName
	if sni == "" {
		sni = c.Hostname
		if verify == "" {
			verify = c.Hostname
		}
	}
	if verify == "" {
		return "", "", errors.New("explicit cover SNI requires verify_name")
	}
	return sni, verify, nil
}

// Fetch public control metadata before attempting any authenticated data session.
func (g *Gateway) checkCapabilities(binder SocketBinder) error {
	if g.cfg.ControlURL == "" {
		return nil
	} // Legacy profiles predate the control plane.
	u, err := url.Parse(g.cfg.ControlURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return errors.New("invalid capabilities HTTPS URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if binder != nil {
		dialer.Control = func(_, _ string, raw syscall.RawConn) error {
			var bindErr error
			err := raw.Control(func(fd uintptr) { bindErr = binder.Bind(int64(fd)) })
			if err != nil {
				return err
			}
			return bindErr
		}
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if g.cfg.CA != "" && !roots.AppendCertsFromPEM([]byte(g.cfg.CA)) {
		return errors.New("invalid control CA")
	}
	// Control uses ordinary URL-host PKI, independent of the VPN SNI/identity.
	tc := &tls.Config{ServerName: u.Hostname(), RootCAs: roots, MinVersion: tls.VersionTLS13, NextProtos: []string{"http/1.1"}}
	tr := &http.Transport{TLSClientConfig: tc, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		c, err := dialer.DialContext(ctx, "tcp4", address)
		if err != nil {
			return nil, err
		}
		return meterBoundConn(c, binder), nil
	}}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return fmt.Errorf("capabilities: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("capabilities HTTP %d", response.StatusCode)
	}
	var caps protocol.Capabilities
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64*1024))
	if err := decoder.Decode(&caps); err != nil {
		return fmt.Errorf("capabilities: %w", err)
	}
	compatibilityErr := caps.CheckData(protocol.DataVersion, g.cfg.AndroidVersionCode)
	if g.cfg.DataVersion != 0 && g.cfg.DataVersion != protocol.DataVersion {
		compatibilityErr = protocol.ErrUpgradeRequired
	}
	if compatibilityErr != nil {
		g.emit("upgrade_required", map[string]any{"min_android_version_code": caps.MinAndroidVersionCode, "data_version": caps.DataVersion})
		return compatibilityErr
	}
	return nil
}
