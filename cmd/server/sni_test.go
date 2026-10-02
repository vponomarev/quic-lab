package main

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSNIFrontendAndPassthrough(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "nginx") }))
	defer backend.Close()
	seed := httptest.NewTLSServer(http.NotFoundHandler())
	cert := seed.TLS.Certificates[0]
	seed.Close()
	raw, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	front, e := newSNIRouter(ctx, raw, "lab.test", strings.Replace(backend.Listener.Addr().String(), "127.0.0.1", "localhost", 1))
	if e != nil {
		t.Fatal(e)
	}
	defer front.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "lab") }), TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}}}
	defer server.Close()
	go server.ServeTLS(front, "", "")
	for _, test := range []struct{ host, want string }{{"lab.test", "lab"}, {"other.test", "nginx"}, {"", "nginx"}} {
		conn, e := tls.Dial("tcp", raw.Addr().String(), &tls.Config{ServerName: test.host, InsecureSkipVerify: true})
		if e != nil {
			t.Fatal(e)
		}
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		io.WriteString(conn, "GET / HTTP/1.0\r\nHost: "+test.host+"\r\n\r\n")
		b, e := io.ReadAll(conn)
		conn.Close()
		if e != nil {
			t.Fatal(e)
		}
		if !strings.HasSuffix(string(b), test.want) {
			t.Fatalf("%s: %q", test.host, b)
		}
	}
}

func TestConfiguredCoverSNIAndFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "fallback") }))
	defer backend.Close()
	seed := httptest.NewTLSServer(http.NotFoundHandler())
	cert := seed.TLS.Certificates[0]
	seed.Close()
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	front, err := newSNIRouter(ctx, raw, "real.test", backend.Listener.Addr().String(), "cover.test")
	if err != nil {
		raw.Close()
		t.Fatal(err)
	}
	defer front.Close()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "vpn") }), TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}}}
	defer srv.Close()
	go srv.ServeTLS(front, "", "")
	for _, tt := range []struct{ host, want string }{{"real.test", "vpn"}, {"cover.test", "vpn"}, {"unconfigured.test", "fallback"}, {"", "fallback"}} {
		c, err := tls.Dial("tcp", raw.Addr().String(), &tls.Config{ServerName: tt.host, InsecureSkipVerify: true})
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(3 * time.Second))
		io.WriteString(c, "GET / HTTP/1.0\r\nHost: "+tt.host+"\r\n\r\n")
		b, err := io.ReadAll(c)
		c.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(string(b), tt.want) {
			t.Fatalf("SNI %q: %s", tt.host, b)
		}
	}
}
