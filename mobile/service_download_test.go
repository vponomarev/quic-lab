package mobile

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

type transferTrustFixture struct {
	url string
	ca  string
}

func (p *transferTrustFixture) ApprovedURL(kind, raw string) bool {
	return kind == "apk" && raw == p.url+"/apk"
}
func (p *transferTrustFixture) Credentials(ref string) (string, error) {
	v, _ := json.Marshal(map[string]string{"ca": p.ca})
	return string(v), nil
}
func (p *transferTrustFixture) ResolveAddress(host string, port int) (string, error) {
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}
func serviceFixture(t *testing.T, limit int64, h http.Handler) (*ServiceTransfer, *TrafficBudget, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	b, _ := NewTrafficBudget("test", limit)
	s := NewServiceTransfer(b, nil)
	s.SetTrust(&transferTrustFixture{srv.URL, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))})
	return s, b, srv
}
func serviceRequest(id, url, out string) string {
	v, _ := json.Marshal(map[string]string{"id": id, "kind": "apk", "https_url": url, "output_file": out})
	return string(v)
}
func TestGrantAppliesToSingleOperation(t *testing.T) {
	s, b, srv := serviceFixture(t, 1, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("apk")) }))
	b.ledger.ObserveReceived(1, "user")
	b.meter.Check()
	out := filepath.Join(t.TempDir(), "apk")
	req := serviceRequest("apk-1", srv.URL+"/apk", out)
	binder := b.Bind(nil, "cell")
	if s.Fetch(req, binder) == nil {
		t.Fatal("no consent allowed")
	}
	b.GrantTransfer("apk-1")
	if s.Fetch(serviceRequest("apk-2", srv.URL+"/apk", out), binder) == nil {
		t.Fatal("grant escaped operation")
	}
	if e := s.Fetch(req, binder); e != nil {
		t.Fatal(e)
	}
	if b.CellAllowed() {
		t.Fatal("grant enabled VPN")
	}
	if b.ledger.Snapshot().Used <= 4 {
		t.Fatal("physical TLS bytes unaccounted")
	}
	if v, _ := os.ReadFile(out); string(v) != "apk" {
		t.Fatal(string(v))
	}
	if s.Fetch(req, binder) == nil {
		t.Fatal("completed consent reusable")
	}
}
func TestNoCrossOriginRedirect(t *testing.T) {
	for _, location := range []string{"https://other.invalid/apk", "http://localhost/apk"} {
		t.Run(location, func(t *testing.T) {
			s, b, srv := serviceFixture(t, 0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, location, 302) }))
			out := filepath.Join(t.TempDir(), "apk")
			if s.Fetch(serviceRequest("redirect", srv.URL+"/apk", out), b.Bind(nil, "wifi")) == nil {
				t.Fatal("unsafe redirect accepted")
			}
			if _, e := os.Stat(out); !os.IsNotExist(e) {
				t.Fatal("partial published")
			}
		})
	}
}
func TestServiceRejectsUntrustedEndpoint(t *testing.T) {
	s, b, srv := serviceFixture(t, 0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("request reached network") }))
	if s.Fetch(serviceRequest("arbitrary", srv.URL+"/anything", filepath.Join(t.TempDir(), "apk")), b.Bind(nil, "wifi")) == nil {
		t.Fatal("untrusted URL accepted")
	}
}
func TestTransferCancelRevokesGrant(t *testing.T) {
	b, _ := NewTrafficBudget("test", 1)
	b.GrantTransfer("cancel")
	NewServiceTransfer(b, nil).Cancel("cancel")
	if b.transferGranted("cancel") {
		t.Fatal("grant retained")
	}
}

func TestTransferCanceledBeforeClaimDoesNotStart(t *testing.T) {
	s, b, srv := serviceFixture(t, 0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("canceled operation reached network") }))
	s.Cancel("cancel-before-claim")
	b.GrantTransfer("cancel-before-claim")
	if s.Fetch(serviceRequest("cancel-before-claim", srv.URL+"/apk", filepath.Join(t.TempDir(), "apk")), b.Bind(nil, "wifi")) == nil {
		t.Fatal("canceled transfer started")
	}
	if b.transferGranted("cancel-before-claim") {
		t.Fatal("canceled attempt retained grant")
	}
}
func TestServiceInvalidAttemptRevokesPendingGrant(t *testing.T) {
	s, b, srv := serviceFixture(t, 1, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("invalid endpoint reached network") }))
	b.GrantTransfer("invalid")
	if s.Fetch(serviceRequest("invalid", srv.URL+"/other", filepath.Join(t.TempDir(), "apk")), b.Bind(nil, "cell")) == nil {
		t.Fatal("invalid allowed")
	}
	if b.transferGranted("invalid") {
		t.Fatal("invalid attempt retained grant")
	}
}
func TestTransferCancelClosesGrantedInFlightSocket(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	s, b, srv := serviceFixture(t, 1, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		close(entered)
		<-release
	}))
	b.ledger.ObserveReceived(1, "user")
	b.meter.Check()
	b.GrantTransfer("cancel-active")
	out := filepath.Join(t.TempDir(), "apk")
	done := make(chan error, 1)
	go func() { done <- s.Fetch(serviceRequest("cancel-active", srv.URL+"/apk", out), b.Bind(nil, "cell")) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("transfer never reached server")
	}
	s.Cancel("cancel-active")
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("canceled completed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel failed to interrupt")
	}
	if _, e := os.Stat(out); !os.IsNotExist(e) {
		t.Fatal("partial file published")
	}
	if b.CellAllowed() {
		t.Fatal("cancel enabled VPN")
	}
}
func TestServiceTLSIdentityAndFailurePreserveOutput(t *testing.T) {
	s, b, srv := serviceFixture(t, 0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted TLS reached handler") }))
	s.SetTrust(&transferTrustFixture{url: srv.URL})
	out := filepath.Join(t.TempDir(), "apk")
	os.WriteFile(out, []byte("previous"), 0600)
	if s.Fetch(serviceRequest("wrong-ca", srv.URL+"/apk", out), b.Bind(nil, "wifi")) == nil {
		t.Fatal("untrusted TLS accepted")
	}
	if v, _ := os.ReadFile(out); string(v) != "previous" {
		t.Fatal("previous output corrupted")
	}
}

type authenticatedTransferTrust struct {
	transferTrustFixture
	token string
}

func (p *authenticatedTransferTrust) ApprovedURL(kind, raw string) bool {
	return (kind == "apk" || kind == "config") && raw == p.url+"/"+kind
}
func (p *authenticatedTransferTrust) Credentials(ref string) (string, error) {
	v, _ := json.Marshal(map[string]string{"token": p.token, "ca": p.ca})
	return string(v), nil
}
func TestServiceBearerIsPrivateAndConfigScoped(t *testing.T) {
	token := "synthetic-private-device-token"
	s, b, srv := serviceFixture(t, 0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apk" && r.Header.Get("Authorization") != "" {
			t.Error("APK received device bearer")
		}
		if r.URL.Path == "/config" && r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("config missing device authentication")
		}
		w.Write([]byte("body"))
	}))
	s.SetTrust(&authenticatedTransferTrust{transferTrustFixture{srv.URL, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))}, token})
	for _, kind := range []string{"apk", "config"} {
		request := map[string]string{"id": "auth-" + kind, "kind": kind, "https_url": srv.URL + "/" + kind, "output_file": filepath.Join(t.TempDir(), kind), "device_auth_ref": "profile"}
		raw, _ := json.Marshal(request)
		if e := s.Fetch(string(raw), b.Bind(nil, "wifi")); e != nil {
			t.Fatal(e)
		}
	}
}

type eventListenerFunc func(string)

func (f eventListenerFunc) OnEvent(raw string) { f(raw) }
func TestServiceConsentListenerCanGrantSameIDRetry(t *testing.T) {
	s, b, srv := serviceFixture(t, 1, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("apk")) }))
	b.ledger.ObserveReceived(1, "user")
	b.meter.Check()
	out := filepath.Join(t.TempDir(), "apk")
	req := serviceRequest("listener-retry", srv.URL+"/apk", out)
	retry := make(chan error, 1)
	s.sink = eventListenerFunc(func(raw string) {
		var event struct {
			State string `json:"state"`
		}
		json.Unmarshal([]byte(raw), &event)
		if event.State == "consent_required" {
			retry <- b.GrantTransfer("listener-retry")
		}
	})
	if s.Fetch(req, b.Bind(nil, "cell")) == nil {
		t.Fatal("consent bypassed")
	}
	if e := <-retry; e != nil {
		t.Fatal("terminal callback grant rejected:", e)
	}
	if !b.transferGranted("listener-retry") {
		t.Fatal("trailing cleanup erased retry grant")
	}
	if e := s.Fetch(req, b.Bind(nil, "cell")); e != nil {
		t.Fatal("same ID retry failed:", e)
	}
}
func TestServiceAuthenticatedRedirectRequiresApprovedConfigTarget(t *testing.T) {
	for _, target := range []string{"/apk", "/other"} {
		t.Run(target, func(t *testing.T) {
			s, b, srv := serviceFixture(t, 0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/config" {
					http.Redirect(w, r, target, 302)
					return
				}
				t.Error("unapproved redirect reached endpoint with bearer:", r.Header.Get("Authorization"))
				w.Write([]byte("body"))
			}))
			s.SetTrust(&authenticatedTransferTrust{transferTrustFixture{srv.URL, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))}, "synthetic-token"})
			req, _ := json.Marshal(map[string]string{"id": "config-redirect", "kind": "config", "https_url": srv.URL + "/config", "output_file": filepath.Join(t.TempDir(), "config"), "device_auth_ref": "profile"})
			if s.Fetch(string(req), b.Bind(nil, "wifi")) == nil {
				t.Fatal("unapproved config redirect accepted")
			}
		})
	}
}
func TestTransferCancellationWinsBeforePublication(t *testing.T) {
	s, b, _ := serviceFixture(t, 0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.claimTransfer("publish-cancel", cancel)
	defer b.finishTransfer("publish-cancel")
	dir := t.TempDir()
	source := filepath.Join(dir, "temporary")
	out := filepath.Join(dir, "apk")
	os.WriteFile(source, []byte("new"), 0600)
	os.WriteFile(out, []byte("previous"), 0600)
	s.Cancel("publish-cancel")
	if e := s.publishTransfer(ctx, "publish-cancel", source, out); e == nil {
		t.Fatal("cancellation lost before publication")
	}
	if v, _ := os.ReadFile(out); string(v) != "previous" {
		t.Fatal("canceled publication replaced output")
	}
}

type approvedRedirectTrust struct{ authenticatedTransferTrust }

func (p *approvedRedirectTrust) ApprovedURL(kind, raw string) bool {
	return kind == "config" && (raw == p.url+"/config" || raw == p.url+"/config-approved")
}
func TestServiceApprovedSameOriginConfigRedirect(t *testing.T) {
	s, b, srv := serviceFixture(t, 0, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config" {
			http.Redirect(w, r, "/config-approved", 302)
			return
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Error("approved config lost bearer")
		}
		w.Write([]byte("configuration"))
	}))
	s.SetTrust(&approvedRedirectTrust{authenticatedTransferTrust{transferTrustFixture{srv.URL, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))}, "synthetic-token"}})
	req, _ := json.Marshal(map[string]string{"id": "approved-config", "kind": "config", "https_url": srv.URL + "/config", "output_file": filepath.Join(t.TempDir(), "config"), "device_auth_ref": "profile"})
	if e := s.Fetch(string(req), b.Bind(nil, "wifi")); e != nil {
		t.Fatal("approved same-origin config rejected:", e)
	}
}
