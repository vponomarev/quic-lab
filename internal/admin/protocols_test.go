package admin

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/url"
	"quiclab/internal/awgserver"
	"strings"
	"testing"
	"time"
)

func awgConfig() *awgserver.Config {
	return &awgserver.Config{Endpoint: "lab.example:51820", Address: "10.77.0.1/29", DNS: "1.1.1.1", AllowedIPs: []string{"0.0.0.0/0"}, Interface: "ql-awg0", MTU: 1280}
}
func TestProtocolRevocationAndProvisioning(t *testing.T) {
	dir := t.TempDir()
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureAWG(awgConfig()); e != nil {
		t.Fatal(e)
	}
	u, e := s.CreateWithProtocols("all", []string{"quic", "https", "awg"})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := s.AWGProfile(u.ID)
	if e != nil || !strings.Contains(raw, "Address = 10.77.0.2/32") {
		t.Fatal("AWG provisioning", e)
	}
	b, _ := pem.Decode([]byte(u.Certificate))
	leaf, e := x509.ParseCertificate(b.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	cs := tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}, VerifiedChains: [][]*x509.Certificate{{leaf, s.ca}}}
	q, h := 0, 0
	release, e := s.RegisterProtocol(cs, "quic", func() { q++ })
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	releaseH, e := s.RegisterProtocol(cs, "https", func() { h++ })
	if e != nil {
		t.Fatal(e)
	}
	defer releaseH()
	if e = s.SetProtocols(u.ID, []string{"https", "awg"}, false); e != nil {
		t.Fatal(e)
	}
	if q != 1 || h != 0 {
		t.Fatal("revocation interrupted wrong transport")
	}
	if _, e = s.RegisterProtocol(cs, "quic", func() {}); e == nil {
		t.Fatal("revoked protocol accepted")
	}
	if e = s.SetProtocols(u.ID, []string{"https", "awg"}, true); e != nil {
		t.Fatal(e)
	}
	if h != 1 {
		t.Fatal("disable did not close HTTPS")
	}
	if _, e = s.AWGProfile(u.ID); e == nil {
		t.Fatal("disabled export accepted")
	}
	if e = s.SetProtocols(u.ID, []string{"https", "awg"}, false); e != nil {
		t.Fatal(e)
	}
	again, _ := s.AWGProfile(u.ID)
	if again != raw {
		t.Fatal("credentials changed on toggle")
	}
	reopened, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	if e = reopened.ConfigureAWG(awgConfig()); e != nil {
		t.Fatal(e)
	}
	again, _ = reopened.AWGProfile(u.ID)
	if again != raw {
		t.Fatal("credentials changed on restart")
	}
	for _, v := range s.List() {
		if v.AWG != nil && (v.AWG.Private != "" || v.AWG.PSK != "") {
			t.Fatal("list leaked keys")
		}
	}
	only, e := s.CreateWithProtocols("awg", []string{"awg"})
	if e != nil {
		t.Fatal(e)
	}
	p, e := NewWeb(config(t), s).profile(only)
	if e != nil || p.Version != 2 || p.Key != "" || p.Certificate != "" || p.QUIC != "" || p.HTTPS != "" || p.AWGConfig == "" {
		t.Fatal("AWG-only export", e)
	}
	old, e := s.Create("legacy")
	if e != nil || !old.Allows("quic") || !old.Allows("https") || old.Allows("awg") {
		t.Fatal("legacy compatibility", e)
	}
	for _, bad := range [][]string{{}, {"awg", "awg"}, {"unknown"}} {
		if _, e = s.CreateWithProtocols("invalid", bad); e == nil {
			t.Fatal("invalid protocols accepted")
		}
	}
}
func TestAWGStatsDeltasAndStaleness(t *testing.T) {
	s, _ := OpenStore(t.TempDir())
	s.ConfigureAWG(awgConfig())
	u, e := s.CreateWithProtocols("stats", []string{"awg"})
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	status := awgserver.Status{Started: now, Updated: now, Peers: []awgserver.PeerStatus{{ID: u.ID, Source: "192.0.2.4:50000", Handshake: now, Activity: now, TX: 100, RX: 200}}}
	s.ObserveAWG(status)
	status.Peers[0].TX += 1000
	status.Peers[0].RX += 2000
	s.ObserveAWG(status)
	snapshot := s.List()[0].Stats
	if snapshot.TX != 1000 || snapshot.RX != 2000 || len(snapshot.Connections) != 1 {
		t.Fatalf("wrong delta %+v", snapshot)
	}
	s.ObserveAWG(awgserver.Status{})
	if len(s.List()[0].Stats.Connections) != 0 {
		t.Fatal("stale worker shown online")
	}
	status.Started = now.Add(time.Second)
	status.Peers[0].TX = 10
	s.ObserveAWG(status)
	if s.List()[0].Stats.TX != 1000 {
		t.Fatal("worker restart counter underflow")
	}
}

func TestProtocolWebExports(t *testing.T) {
	c := config(t)
	c.AWG = awgConfig()
	s, _ := OpenStore(c.DataDir)
	s.ConfigureAWG(c.AWG)
	w := NewWeb(c, s)
	h := w.Handler()
	login := call(h, "POST", "/login", url.Values{"username": {c.Username}, "password": {c.Password}}.Encode(), nil)
	cookie := login.Result().Cookies()[0]
	csrf := w.sessions[cookie.Value].CSRF
	form := url.Values{"csrf": {csrf}, "name": {"multiprotocol"}, "protocols_present": {"1"}, "protocol": {"quic", "https", "awg"}}
	if r := call(h, "POST", "/users/add", form.Encode(), cookie); r.Code != 303 {
		t.Fatal(r.Code, r.Body.String())
	}
	u := s.List()[0]
	form = url.Values{"csrf": {csrf}, "id": {u.ID}}
	r := call(h, "POST", "/users/config", form.Encode(), cookie)
	var p Profile
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &p) != nil || p.Version != 2 || len(p.Transports) != 3 || p.AWGConfig == "" || p.Key == "" {
		t.Fatal("complex export failed")
	}
	form.Set("format", "awg")
	r = call(h, "POST", "/users/config", form.Encode(), cookie)
	if r.Code != 200 || !strings.HasPrefix(r.Body.String(), "[Interface]") {
		t.Fatal("standard export failed")
	}
	r = call(h, "POST", "/users/awg-qr", form.Encode(), cookie)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "data:image/png;base64,") {
		t.Fatal("AWG QR failed")
	}
	form.Del("csrf")
	if r = call(h, "POST", "/users/config", form.Encode(), cookie); r.Code != 403 {
		t.Fatal("export CSRF bypass")
	}
	if r = call(h, "POST", "/users/config", form.Encode(), nil); r.Code != 303 {
		t.Fatal("unauthenticated export")
	}
	form.Set("csrf", csrf)
	form.Set("disabled", "true")
	if r = call(h, "POST", "/users/toggle", form.Encode(), cookie); r.Code != 303 {
		t.Fatal("toggle failed")
	}
	if r = call(h, "POST", "/users/config", form.Encode(), cookie); r.Code == 200 {
		t.Fatal("disabled export")
	}
}
