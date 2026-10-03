package vlessserver

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"os"
	"quiclab/internal/vless"
	"time"
)

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

func readMaterial(path string, private bool) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, errConfig
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > 1024*1024 || (private && st.Mode().Perm()&0077 != 0) {
		return nil, errConfig
	}
	b, e := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if e != nil || len(b) > 1024*1024 {
		return nil, errConfig
	}
	return b, nil
}
func (c Config) serverOptions(ctx context.Context, clients []vless.ServerClient, resolver ipResolver) (vless.ServerOptions, error) {
	o := vless.ServerOptions{}
	if e := c.Validate(); e != nil {
		return o, e
	}
	o = vless.ServerOptions{Listen: c.Listen, Security: c.Security, Mode: c.Mode, Clients: append([]vless.ServerClient(nil), clients...), RealityTarget: c.RealityTarget, RealityServerNames: append([]string(nil), c.RealityServerNames...), RealityShortIDs: append([]string(nil), c.RealityShortIDs...)}
	var e error
	if c.Security == "tls" {
		o.Certificate, e = readMaterial(c.TLSCertificateFile, false)
		if e != nil {
			return vless.ServerOptions{}, e
		}
		o.Key, e = readMaterial(c.TLSKeyFile, true)
	} else {
		o.RealityPrivateKey, e = base64.RawURLEncoding.Strict().DecodeString(c.RealityPrivateKey)
	}
	if e != nil {
		return vless.ServerOptions{}, errConfig
	}
	if c.Mode == "demux-only" {
		host, port, _ := net.SplitHostPort(c.DemuxEndpoint)
		if ip := net.ParseIP(host); ip != nil {
			o.DemuxEndpoint = net.JoinHostPort(ip.String(), port)
		} else {
			lookup, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			addresses, e := resolver.LookupIPAddr(lookup, host)
			if e != nil {
				return vless.ServerOptions{}, errConfig
			}
			for _, a := range addresses {
				if ip := a.IP.To4(); ip != nil && !ip.IsUnspecified() && !ip.IsMulticast() {
					o.DemuxEndpoint = net.JoinHostPort(ip.String(), port)
					break
				}
			}
			if o.DemuxEndpoint == "" {
				return vless.ServerOptions{}, errConfig
			}
		}
	}
	return o, nil
}
