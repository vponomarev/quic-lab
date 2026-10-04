package vlessserver

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"time"
)

func preflightCertificate(c Config) error {
	if c.Security != "tls" {
		return nil
	}
	pair, err := tls.LoadX509KeyPair(c.TLSCertificateFile, c.TLSKeyFile)
	if err != nil || len(pair.Certificate) == 0 {
		return errors.New("VLESS certificate/key unavailable or mismatched")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return errors.New("invalid VLESS certificate")
	}
	now := time.Now()
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return errors.New("VLESS certificate is not currently valid")
	}
	if leaf.VerifyHostname(c.ServerName) != nil {
		return errors.New("VLESS certificate does not cover selected SNI")
	}
	return nil
}
