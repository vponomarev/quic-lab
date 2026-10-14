package vless

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsVLESSFixture(t *testing.T) {
	dir := os.Getenv("QUIC_LAB_WINDOWS_VLESS_FIXTURE")
	if dir == "" {
		t.Skip("explicit Windows fixture")
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute directory required")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	cert, key := fixtureCertificate(t)
	pair, e := tls.X509KeyPair(cert, key)
	if e != nil {
		t.Fatal(e)
	}
	camouflage, e := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13, NextProtos: []string{"h2", "http/1.1"}})
	if e != nil {
		t.Fatal(e)
	}
	defer camouflage.Close()
	go func() {
		for {
			c, e := camouflage.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); c.SetDeadline(time.Now().Add(5 * time.Second)); io.Copy(io.Discard, c) }()
		}
	}()
	k, _ := ecdh.X25519().GenerateKey(rand.Reader)
	var id [16]byte
	rand.Read(id[:])
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	for _, mode := range []string{"tls", "reality"} {
		l, e := net.Listen("tcp4", "0.0.0.0:0")
		if e != nil {
			t.Fatal(e)
		}
		port := l.Addr().(*net.TCPAddr).Port
		l.Close()
		o := ServerOptions{Listen: fmt.Sprintf("0.0.0.0:%d", port), Security: mode, Mode: "standalone", Clients: []ServerClient{{UUID: uuid}}}
		q := url.Values{"security": {mode}, "sni": {"fixture.invalid"}, "type": {"tcp"}, "encryption": {"none"}}
		ca := ""
		if mode == "tls" {
			o.Certificate = cert
			o.Key = key
			ca = string(cert)
		} else {
			o.RealityTarget = camouflage.Addr().String()
			o.RealityServerNames = []string{"fixture.invalid"}
			o.RealityPrivateKey = k.Bytes()
			o.RealityShortIDs = []string{"aabb"}
			o.Clients[0].Flow = "xtls-rprx-vision"
			q.Set("flow", "xtls-rprx-vision")
			q.Set("fp", "chrome")
			q.Set("pbk", base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()))
			q.Set("sid", "aabb")
		}
		server, e := StartServer(context.Background(), o)
		if e != nil {
			t.Fatal(e)
		}
		defer server.Close()
		endpoint := fmt.Sprintf("192.168.5.214:%d", port)
		uri := url.URL{Scheme: "vless", Host: endpoint, User: url.User(uuid), RawQuery: q.Encode()}
		raw, _ := json.Marshal(map[string]any{"transport": "vless", "endpoint": endpoint, "key": uri.String(), "ca": ca, "targets": []string{"1.1.1.0/30"}, "interface_index": 25})
		if e = os.WriteFile(filepath.Join(dir, mode+".json"), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	t.Log("TLS and REALITY fixtures ready; credentials stored privately")
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(filepath.Join(dir, "stop")); e == nil {
			return
		}
		time.Sleep(time.Second)
	}
}
