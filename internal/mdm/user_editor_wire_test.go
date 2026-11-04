package mdm

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"
)

// Opt-in fixture only. Default suites skip it; no production handler is added.
func TestUserEditorDeviceFixture(t *testing.T) {
	endpoint := os.Getenv("MDM_FIXTURE_ENDPOINT")
	if endpoint == "" {
		t.Skip("explicit disposable device fixture required")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || net.ParseIP(u.Hostname()) == nil {
		t.Fatal("invalid fixture endpoint")
	}
	infoPath := os.Getenv("MDM_FIXTURE_INFO")
	if infoPath == "" {
		t.Fatal("fixture info path required")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Disposable MDM editor test"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true, IPAddresses: []net.IP{net.ParseIP(u.Hostname())}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invitation, err := store.CreateInvitation(now, Rights{Config: true})
	if err != nil {
		t.Fatal(err)
	}
	adminToken := randomID() + randomID()
	done := make(chan string, 1)
	var once sync.Once
	mux := http.NewServeMux()
	mux.Handle("/mdm/", NewHandler(store))
	mux.HandleFunc("/fixture/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+adminToken {
			http.Error(w, "denied", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var req struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		state, err := store.DeviceState(req.ID)
		if err != nil {
			http.Error(w, "unknown", 404)
			return
		}
		if r.URL.Path == "/fixture/permission" {
			p, err := store.SetUserConfigEdit(req.ID, state.UserConfigEdit.Revision, req.Enabled)
			if err != nil {
				http.Error(w, "conflict", 409)
				return
			}
			json.NewEncoder(w).Encode(p)
			return
		}
		if r.URL.Path == "/fixture/finish" {
			if state.Desired == nil || state.Report == nil || state.Report.AppliedRevision != state.Desired.Revision || state.UserConfigEdit.Enabled {
				http.Error(w, "not verified", 409)
				return
			}
			count := 0
			for _, e := range state.Events {
				if e.Kind == "config_update" && e.Actor == "user" {
					count++
				}
			}
			if count != 1 {
				http.Error(w, "retry duplicated revision", 409)
				return
			}
			json.NewEncoder(w).Encode(map[string]bool{"verified": true})
			once.Do(func() { done <- req.ID })
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewUnstartedServer(mux)
	server.Listener.Close()
	server.Listener, err = net.Listen("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	defer server.Close()
	raw, _ := json.Marshal(map[string]string{"endpoint": endpoint, "token": invitation.Token, "ca": base64.StdEncoding.EncodeToString(ca), "admin": adminToken})
	if err = os.WriteFile(infoPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(infoPath)
	t.Log("Disposable HTTPS fixture ready")
	select {
	case <-done:
		t.Log("Device save/retry/apply/report/revoke verified")
	case <-time.After(15 * time.Minute):
		t.Fatal("device fixture timed out")
	}
}
