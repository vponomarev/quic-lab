package mobile

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"quiclab/internal/gateway"
	"quiclab/internal/vless"
	"sync/atomic"
	"testing"
	"time"
)

// Explicit opt-in fixture. Credentials live in a private directory and expire
// with the fixture. The inner gateway is loopback-only: a phone cannot bypass VLESS.
func TestRelayAndroidFixture(t *testing.T) {
	dir := os.Getenv("QUIC_LAB_RELAY_ANDROID_FIXTURE")
	if dir == "" {
		t.Skip("explicit Android fixture")
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute fixture directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	pair, cp, kp := testIdentity(t)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(cp))
	inner, e := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13})
	if e != nil {
		t.Fatal(e)
	}
	g, _ := gateway.New("127.0.0.0/8,192.168.5.214/32", slog.New(slog.NewTextHandler(io.Discard, nil)))
	hs := &http.Server{Handler: http.HandlerFunc(g.ServeBondHTTPS)}
	defer hs.Close()
	go hs.Serve(inner)
	disguise, e := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13, NextProtos: []string{"h2", "http/1.1"}})
	if e != nil {
		t.Fatal(e)
	}
	defer disguise.Close()
	go func() {
		for {
			c, e := disguise.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); c.SetDeadline(time.Now().Add(5 * time.Second)); io.Copy(io.Discard, c) }()
		}
	}()
	var id [16]byte
	rand.Read(id[:])
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	var tokenBytes [24]byte
	rand.Read(tokenBytes[:])
	token := base64.RawURLEncoding.EncodeToString(tokenBytes[:])
	proxies := make([]*vless.Server, 0, 2)
	uris := []string{}
	for i := 0; i < 2; i++ {
		slot, e := net.Listen("tcp4", "0.0.0.0:0")
		if e != nil {
			t.Fatal(e)
		}
		port := slot.Addr().(*net.TCPAddr).Port
		slot.Close()
		key, _ := ecdh.X25519().GenerateKey(rand.Reader)
		p, e := vless.StartServer(context.Background(), vless.ServerOptions{Listen: fmt.Sprintf("0.0.0.0:%d", port), Security: "reality", Mode: "demux-only", DemuxEndpoint: inner.Addr().String(), RealityTarget: disguise.Addr().String(), RealityServerNames: []string{"localhost"}, RealityPrivateKey: key.Bytes(), RealityShortIDs: []string{"aabb"}, Clients: []vless.ServerClient{{UUID: uuid}}})
		if e != nil {
			t.Fatal(e)
		}
		defer p.Close()
		proxies = append(proxies, p)
		q := url.Values{"security": {"reality"}, "type": {"tcp"}, "sni": {"localhost"}, "fp": {"chrome"}, "sid": {"aabb"}, "pbk": {base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())}}
		u := url.URL{Scheme: "vless", User: url.User(uuid), Host: fmt.Sprintf("192.168.5.214:%d", port), RawQuery: q.Encode()}
		uris = append(uris, u.String())
	}
	dst, e := net.Listen("tcp4", "0.0.0.0:0")
	if e != nil {
		t.Fatal(e)
	}
	defer dst.Close()
	udp, e := net.ListenPacket("udp4", "0.0.0.0:0")
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, a, e := udp.ReadFrom(buf)
			if e != nil {
				return
			}
			udp.WriteTo(buf[:n], a)
		}
	}()
	var accepts atomic.Int32
	var bytes atomic.Int64
	go func() {
		for {
			c, e := dst.Accept()
			if e != nil {
				return
			}
			accepts.Add(1)
			go func() { defer c.Close(); n, _ := io.Copy(c, c); bytes.Add(n) }()
		}
	}()
	ctl, e := tls.Listen("tcp4", "0.0.0.0:0", &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13})
	if e != nil {
		t.Fatal(e)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.Method == "POST" {
			switch r.URL.Path {
			case "/stop/0":
				proxies[0].Close()
			case "/stop/1":
				proxies[1].Close()
			default:
				http.NotFound(w, r)
				return
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"accepts": accepts.Load(), "bytes": bytes.Load()})
	})
	control := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	defer control.Close()
	go control.Serve(ctl)
	payload := map[string]any{"group": map[string]any{"vless_uris": uris, "vless_config": mustImportRelay(t, uris[0])}, "target": map[string]any{"transport": "https", "endpoint": inner.Addr().String(), "hostname": "localhost", "certificate": cp, "key": kp, "ca": cp}, "probeHost": "192.168.5.214", "udpPort": udp.LocalAddr().(*net.UDPAddr).Port, "probePort": dst.Addr().(*net.TCPAddr).Port, "control": fmt.Sprintf("https://192.168.5.214:%d", ctl.Addr().(*net.TCPAddr).Port), "token": token}
	raw, _ := json.Marshal(payload)
	if e = os.WriteFile(filepath.Join(dir, "fixture.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	t.Log("Android fixture ready; private credentials written")
	until := time.Now().Add(15 * time.Minute)
	for time.Now().Before(until) {
		if _, e = os.Stat(filepath.Join(dir, "stop")); e == nil {
			return
		}
		time.Sleep(time.Second)
	}
}
func mustImportRelay(t *testing.T, uri string) string {
	t.Helper()
	s, e := ImportVLESSConfig(uri)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
