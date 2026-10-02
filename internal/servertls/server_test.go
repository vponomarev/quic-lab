package servertls

import (
	"context"
	"crypto/tls"
	"github.com/quic-go/quic-go"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConfiguredServerSNIQUICAndHTTPS(t *testing.T) {
	pair, _ := identity(t, false)
	for _, network := range []string{"quic", "https"} {
		t.Run(network, func(t *testing.T) {
			tc := AllowServerNames(&tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13, NextProtos: []string{"test-vpn"}}, "vpn.test", []string{"cover.test"})
			var endpoint string
			var httpsServer *httptest.Server
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if network == "quic" {
				ln, err := quic.ListenAddr("127.0.0.1:0", tc, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				endpoint = ln.Addr().String()
				go func() {
					for {
						_, err := ln.Accept(ctx)
						if err != nil {
							return
						}
					}
				}()
			} else {
				tc.NextProtos = []string{"http/1.1"}
				httpsServer = httptest.NewUnstartedServer(http.NotFoundHandler())
				httpsServer.TLS = tc
				httpsServer.StartTLS()
				defer httpsServer.Close()
				endpoint = httpsServer.URL
			}
			for _, test := range []struct {
				name    string
				allowed bool
			}{{"vpn.test", true}, {"cover.test", true}, {"foreign.test", false}, {"", false}} {
				clientTLS := &tls.Config{ServerName: test.name, InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, NextProtos: []string{"test-vpn"}}
				var err error
				if network == "quic" {
					var c *quic.Conn
					c, err = quic.DialAddr(ctx, endpoint, clientTLS, nil)
					if c != nil {
						c.CloseWithError(0, "")
					}
				} else {
					clientTLS.NextProtos = []string{"http/1.1"}
					tr := &http.Transport{TLSClientConfig: clientTLS}
					var r *http.Response
					r, err = (&http.Client{Transport: tr, Timeout: 3 * time.Second}).Get(endpoint)
					if r != nil {
						r.Body.Close()
					}
					tr.CloseIdleConnections()
				}
				if (err == nil) != test.allowed {
					t.Fatalf("%s SNI %q allowed=%v error=%v", network, test.name, test.allowed, err)
				}
			}
		})
	}
}

func TestEmptySNIPolicyPreservesLegacy(t *testing.T) {
	cfg := AllowServerNames(&tls.Config{}, "vpn.test", nil)
	if cfg.GetConfigForClient != nil {
		if _, err := cfg.GetConfigForClient(&tls.ClientHelloInfo{ServerName: "foreign.test"}); err != nil {
			t.Fatalf("legacy SNI changed: %v", err)
		}
	}
}
