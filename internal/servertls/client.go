// Package servertls separates the visible SNI from the authenticated VPN identity.
package servertls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"
	"time"
)

type ClientOptions struct {
	ServerName, VerifyName string
	Roots                  *x509.CertPool
	Certificate            *tls.Certificate
}

// ClientConfig always authenticates the configured real identity, including on
// session resumption. InsecureSkipVerify disables only Go's SNI-name coupling;
// the mandatory VerifyConnection below performs chain, name, EKU and time checks.
func ClientConfig(o ClientOptions) (*tls.Config, error) {
	if strings.TrimSpace(o.VerifyName) == "" || strings.TrimSpace(o.ServerName) == "" || strings.ContainsAny(o.VerifyName+o.ServerName, "\r\n\t /\\") {
		return nil, errors.New("VPN SNI and verified server name required")
	}
	roots := o.Roots
	if roots != nil {
		roots = roots.Clone()
	}
	cfg := &tls.Config{ServerName: o.ServerName, MinVersion: tls.VersionTLS13, InsecureSkipVerify: true}
	if o.Certificate != nil {
		cfg.Certificates = []tls.Certificate{*o.Certificate}
	}
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("VPN server certificate missing")
		}
		intermediates := x509.NewCertPool()
		for _, c := range cs.PeerCertificates[1:] {
			intermediates.AddCert(c)
		}
		_, err := cs.PeerCertificates[0].Verify(x509.VerifyOptions{DNSName: o.VerifyName, Roots: roots, Intermediates: intermediates, CurrentTime: time.Now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
		return err
	}
	return cfg, nil
}
