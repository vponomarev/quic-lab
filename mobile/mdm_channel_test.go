package mobile

import (
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type mdmResolverFixture struct{ calls atomic.Int32 }

func (r *mdmResolverFixture) ResolveAddress(host string, port int) (string, error) {
	r.calls.Add(1)
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}
func mdmFixture(t *testing.T, h http.Handler) (*MDMChannel, *TrafficBudget, *httptest.Server, *mdmResolverFixture) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	b, _ := NewTrafficBudget("mdm-test", 0)
	r := &mdmResolverFixture{}
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	c, e := NewMDMChannel(b, r, ca)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(c.Close)
	return c, b, srv, r
}
func TestMDMIndependentAndMetered(t *testing.T) {
	c, b, s, _ := mdmFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("wrong request")
		}
		w.Write([]byte(`{"version":1}`))
	}))
	for i := 0; i < 2; i++ {
		v, e := c.Exchange(s.URL+"/mdm/v1/sync", "test-secret", "{}", b.Bind(nil, "cell"))
		if e != nil || v != `{"version":1}` {
			t.Fatalf("%q %v", v, e)
		}
	}
	if b.ledger.Snapshot().Used <= 30 {
		t.Fatal("TLS traffic not accounted")
	}
}
func TestMDMBudgetBeforeDNS(t *testing.T) {
	c, b, s, r := mdmFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected traffic") }))
	b.ledger.ObserveReceived(1, "user")
	// A blocked finite budget must reject even DNS and TCP handshake.
	finite, _ := NewTrafficBudget("blocked", 1)
	finite.ledger.ObserveReceived(1, "user")
	finite.meter.Check()
	c.budget = finite
	if _, e := c.Exchange(s.URL+"/mdm/v1/sync", "secret", "{}", finite.Bind(nil, "cell")); e == nil {
		t.Fatal("budget bypass")
	}
	if r.calls.Load() != 0 {
		t.Fatal("DNS leaked")
	}
}
func TestMDMRejectRedirectAndUntrustedTLS(t *testing.T) {
	var hits atomic.Int32
	dest := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer dest.Close()
	c, b, s, _ := mdmFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dest.URL, 307) }))
	if _, e := c.Exchange(s.URL+"/mdm/v1/sync", "secret", "{}", b.Bind(nil, "wifi")); e == nil {
		t.Fatal("redirect accepted")
	}
	if hits.Load() != 0 {
		t.Fatal("credential redirect")
	}
	untrusted, _ := NewMDMChannel(b, &mdmResolverFixture{}, "")
	defer untrusted.Close()
	if _, e := untrusted.Exchange(s.URL+"/mdm/v1/sync", "secret", "{}", b.Bind(nil, "wifi")); e == nil {
		t.Fatal("untrusted TLS accepted")
	}
}
func TestMDMCancelPoll(t *testing.T) {
	started := make(chan struct{})
	c, b, s, _ := mdmFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	done := make(chan error, 1)
	go func() { _, e := c.Exchange(s.URL+"/mdm/v1/sync", "secret", "{}", b.Bind(nil, "wifi")); done <- e }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	c.Close()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancel succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("poll leaked")
	}
	if _, e := c.Exchange(s.URL+"/mdm/v1/sync", "secret", "{}", b.Bind(nil, "wifi")); e == nil {
		t.Fatal("closed channel reused")
	}

}
func TestMDMReportOperation(t *testing.T) {
	c, b, s, _ := mdmFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"accepted":true}`)) }))
	if _, e := c.Exchange(s.URL+"/mdm/v1/report", "secret", "{}", b.Bind(nil, "wifi")); e != nil {
		t.Fatal(e)
	}
}
