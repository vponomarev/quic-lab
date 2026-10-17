package mobile

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"net"
	"quiclab/internal/bond"
	"quiclab/internal/servertls"
	"time"
)

func (p *VLESSProbe) probeRelay(ctx context.Context, raw json.RawMessage) string {
	var cfg gatewayConfig
	if json.Unmarshal(raw, &cfg) != nil {
		return "relay_auth"
	}
	pair, e := tls.X509KeyPair([]byte(cfg.Certificate), []byte(cfg.Key))
	if e != nil {
		return "relay_auth"
	}
	host, _, e := net.SplitHostPort(cfg.Endpoint)
	if e != nil {
		return "relay_auth"
	}
	if cfg.Hostname == "" {
		cfg.Hostname = host
	}
	s, v, e := cfg.tlsNames()
	if e != nil {
		return "relay_auth"
	}
	var roots *x509.CertPool
	if cfg.CA != "" {
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(cfg.CA)) {
			return "relay_auth"
		}
	}
	tc, e := servertls.ClientConfig(servertls.ClientOptions{ServerName: s, VerifyName: v, Roots: roots, Certificate: &pair})
	if e != nil {
		return "relay_auth"
	}
	g := NewGateway(nil)
	g.ctx, g.cancel = context.WithCancel(ctx)
	g.cfg = cfg
	g.tls = tc
	g.bond = bond.NewMux(g.ctx, true)
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		g.Stop()
		return "transport"
	}
	g.bondToken = hex.EncodeToString(secret)
	defer g.Stop()
	info := bond.PathInfo{ID: "subscription-probe", ProfileID: "subscription-probe", Network: "wifi", Generation: 1}
	if e = g.addBondHTTPSWithDial(ctx, cfg.Endpoint, info, nil, true, p.client.DialContext, nil); e != nil {
		if ctx.Err() != nil {
			return probeError(ctx, e)
		}
		if probeError(ctx, e) == "target_tls" {
			return "relay_auth"
		}
		return "transport"
	}
	if e = g.bond.Session.ProbeData(info.ID); e != nil {
		return "transport"
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		for _, path := range g.bond.Session.Stats().Paths {
			if path.Name == info.ID && path.DataProbes > 0 {
				return "success"
			}
		}
		select {
		case <-ctx.Done():
			return probeError(ctx, ctx.Err())
		case <-tick.C:
		}
	}
}
