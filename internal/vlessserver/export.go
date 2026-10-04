package vlessserver

import (
	"errors"
	"net/url"
	"quiclab/internal/vless"
)

func (c Config) clientURI(uuid, name string) (string, error) {
	q := url.Values{"encryption": {"none"}, "type": {"tcp"}, "security": {c.Security}, "sni": {c.ServerName}}
	if c.Fingerprint != "" {
		q.Set("fp", c.Fingerprint)
	}
	if c.Flow != "" {
		q.Set("flow", c.Flow)
	}
	if c.Security == "reality" {
		pub, e := c.publicKey()
		if e != nil || len(c.RealityShortIDs) == 0 {
			return "", errConfig
		}
		q.Set("pbk", pub)
		id := c.RealityExportShortID
		if id == "" {
			id = c.RealityShortIDs[0]
		}
		q.Set("sid", id)
		spider, err := vless.NormalizeSpiderX(c.RealitySpiderX)
		if err != nil {
			return "", err
		}
		q.Set("spx", spider)
	}
	u := url.URL{Scheme: "vless", User: url.User(uuid), Host: c.Endpoint, RawQuery: q.Encode(), Fragment: name}
	return u.String(), nil
}

// ClientURI exports one identity and public settings; never server private keys.
func (c Config) ClientURI(uuid, name string) (string, error) {
	if e := c.Validate(); e != nil {
		return "", e
	}
	raw, e := c.clientURI(uuid, name)
	if e != nil {
		return "", e
	}
	if _, e = vless.ParseImport(raw); e != nil {
		return "", errors.New("invalid VLESS export identity or name")
	}
	return raw, nil
}
