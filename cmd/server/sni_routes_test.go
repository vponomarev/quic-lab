package main

import (
	"bufio"
	"context"
	"crypto/tls"
	proxyproto "github.com/pires/go-proxyproto"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"quiclab/internal/protocol"
	"testing"
	"time"
)

func TestSNIRouteProxyV2BeforeTLS(t *testing.T) {
	seed := httptest.NewTLSServer(http.NotFoundHandler())
	cert := seed.TLS.Certificates[0]
	seed.Close()
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	type observed struct {
		source, dest, sni string
		err               error
	}
	got := make(chan observed, 1)
	go func() {
		c, e := backend.Accept()
		if e != nil {
			got <- observed{err: e}
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(c)
		h, e := proxyproto.Read(reader)
		if e != nil {
			got <- observed{err: e}
			return
		}
		tc := tls.Server(&replayConn{c, reader}, &tls.Config{Certificates: []tls.Certificate{cert}})
		e = tc.Handshake()
		if e == nil {
			_, e = tc.Write([]byte("ok"))
		}
		got <- observed{h.SourceAddr.String(), h.DestinationAddr.String(), tc.ConnectionState().ServerName, e}
	}()
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	front, err := newSNIRouterWithRoutes(context.Background(), raw, "local.test", "", []sniRoute{{ServerNames: []string{"remote.test"}, Target: backend.Addr().String(), ProxyProtocol: true}})
	if err != nil {
		raw.Close()
		t.Fatal(err)
	}
	defer front.Close()
	c, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	source := c.LocalAddr().String()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	tc := tls.Client(c, &tls.Config{ServerName: "remote.test", InsecureSkipVerify: true})
	b := make([]byte, 2)
	if _, err = io.ReadFull(tc, b); err != nil {
		t.Fatal(err)
	}
	if string(b) != "ok" {
		t.Fatalf("payload %q", b)
	}
	result := <-got
	if result.err != nil || result.source != source || result.dest != raw.Addr().String() || result.sni != "remote.test" {
		t.Fatalf("unexpected forwarded connection: %+v", result)
	}
}

func TestSNIRouteValidation(t *testing.T) {
	valid := sniRoute{ServerNames: []string{"remote.test"}, Target: "127.0.0.1:9444", ProxyProtocol: true}
	for _, tc := range []struct {
		name   string
		routes []sniRoute
		bad    bool
	}{
		{"valid", []sniRoute{valid}, false},
		{"local collision", []sniRoute{{ServerNames: []string{"LOCAL.test"}, Target: valid.Target}}, true},
		{"cover collision", []sniRoute{{ServerNames: []string{"cover.test"}, Target: valid.Target}}, true},
		{"duplicate", []sniRoute{valid, valid}, true},
		{"public proxy", []sniRoute{{ServerNames: valid.ServerNames, Target: "0.0.0.0:9444", ProxyProtocol: true}}, true},
		{"dns proxy", []sniRoute{{ServerNames: valid.ServerNames, Target: "localhost:9444", ProxyProtocol: true}}, true},
		{"wildcard", []sniRoute{{ServerNames: []string{"*.test"}, Target: valid.Target}}, true},
		{"empty names", []sniRoute{{Target: valid.Target}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateSNIRoutes("local.test", []string{"cover.test"}, tc.routes)
			if (err != nil) != tc.bad {
				t.Fatalf("error=%v want bad=%v", err, tc.bad)
			}
		})
	}
}

func TestConfiguredSNIRoutesRejectPublicLoops(t *testing.T) {
	c := serverConfig{Listen: "127.0.0.1:4433", EphemeralCert: true, TLSHost: "local.test", HTTPSListen: "0.0.0.0:443", Capabilities: protocol.DefaultCapabilities(), TLSRoutes: []sniRoute{{ServerNames: []string{"remote.test"}, Target: "127.0.0.1:9444", ProxyProtocol: true}}}
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	c.TLSRoutes[0].Target = "127.0.0.1:443"
	if c.validate() == nil {
		t.Fatal("accepted loop to own public listener")
	}
	c.TLSRoutes[0].Target = "127.0.0.1:9444"
	c.HTTPSListen = ""
	if c.validate() == nil {
		t.Fatal("accepted routes without listener")
	}
}
