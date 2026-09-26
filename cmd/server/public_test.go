package main

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"quiclab/internal/admin"
	"quiclab/internal/gateway"
)

func TestSharedHTTPSPublicAndMTLS(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := admin.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.Create("test")
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := tls.X509KeyPair([]byte(user.Certificate), []byte(user.Key))
	if err != nil {
		t.Fatal(err)
	}
	seed := httptest.NewTLSServer(http.NotFoundHandler())
	cert := seed.TLS.Certificates[0]
	seed.Close()
	gw, err := gateway.New("127.0.0.1/32", log)
	if err != nil {
		t.Fatal(err)
	}
	gw.Register = store.Register
	cfg := admin.Config{PublicURL: "https://example.test/lab/", Username: "admin", Password: "test-password-long", Echo: admin.Profile{Endpoint: "example.test:4433", Hostname: "example.test"}}
	ts := httptest.NewUnstartedServer(publicHandler(admin.NewWeb(cfg, store).Handler(), cfg.PublicURL, log, http.HandlerFunc(gw.WebSocket)))
	ts.TLS = publicTLS(store.TLS(cert))
	ts.StartTLS()
	defer ts.Close()
	client := ts.Client()
	for _, path := range []string{"/lab/", "/lab/login", "/vpn-demo/"} {
		r, e := client.Get(ts.URL + path)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("anonymous %s: %d", path, r.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", ts.URL+"/tunnel", nil)
	req.Header.Set("X-SSL-Client-Verify", "SUCCESS")
	req.Header.Set("X-SSL-Client-Cert", strings.ReplaceAll(user.Certificate, "\n", ""))
	r, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatalf("anonymous tunnel: %d", r.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "wss" + strings.TrimPrefix(ts.URL, "https")
	echo, _, err := websocket.Dial(ctx, wsURL+"/echo", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		t.Fatal("anonymous echo", err)
	}
	echo.CloseNow()
	mt := client.Transport.(*http.Transport).Clone()
	mt.TLSClientConfig.Certificates = []tls.Certificate{clientCert}
	mt.TLSClientConfig.ClientSessionCache = tls.NewLRUClientSessionCache(4)
	mtlsClient := &http.Client{Transport: mt}
	defer mt.CloseIdleConnections()
	vpn, _, err := websocket.Dial(ctx, wsURL+"/tunnel", &websocket.DialOptions{HTTPClient: mtlsClient})
	if err != nil {
		t.Fatal("authenticated tunnel", err)
	}
	defer vpn.CloseNow()
	if err := store.Delete(user.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := vpn.Read(ctx); err == nil {
		t.Fatal("revoked session survived")
	}
	mt.CloseIdleConnections()
	if conn, _, err := websocket.Dial(ctx, wsURL+"/tunnel", &websocket.DialOptions{HTTPClient: mtlsClient}); err == nil {
		conn.CloseNow()
		t.Fatal("revoked client reconnected")
	}
	// Revocation must not prevent anonymous access to public routes.
	r, err = client.Get(ts.URL + "/lab/")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	foreignStore, err := admin.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreignUser, err := foreignStore.Create("foreign")
	if err != nil {
		t.Fatal(err)
	}
	foreignCert, err := tls.X509KeyPair([]byte(foreignUser.Certificate), []byte(foreignUser.Key))
	if err != nil {
		t.Fatal(err)
	}
	bad := client.Transport.(*http.Transport).Clone()
	bad.TLSClientConfig.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return &foreignCert, nil }
	defer bad.CloseIdleConnections()
	if r, err := (&http.Client{Transport: bad}).Get(ts.URL + "/lab/"); err == nil {
		r.Body.Close()
		t.Fatal("untrusted certificate accepted on shared TLS listener")
	}

}
