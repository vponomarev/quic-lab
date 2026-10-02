package mobile

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"quiclab/internal/trafficbudget"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ServiceTransferTrust is implemented by the private Android profile coordinator.
// ApprovedURL must compare with the endpoint of an already verified profile.
// Credentials returns private JSON {token,ca}; neither value enters events.
// ResolveAddress resolves using the same physical network as the socket binder.
type ServiceTransferTrust interface {
	ApprovedURL(kind, raw string) bool
	Credentials(reference string) (string, error)
	ResolveAddress(host string, port int) (string, error)
}
type ServiceTransfer struct {
	budget   *TrafficBudget
	sink     EventSink
	mu       sync.Mutex
	trust    ServiceTransferTrust
	canceled map[string]bool
	eventMu  sync.Mutex
}

func NewServiceTransfer(b *TrafficBudget, sink EventSink) *ServiceTransfer {
	return &ServiceTransfer{budget: b, sink: sink}
}
func (s *ServiceTransfer) SetTrust(trust ServiceTransferTrust) {
	s.mu.Lock()
	s.trust = trust
	s.mu.Unlock()
}
func (s *ServiceTransfer) Cancel(id string) {
	s.mu.Lock()
	if s.canceled == nil {
		s.canceled = make(map[string]bool)
	}
	s.canceled[id] = true
	s.mu.Unlock()
	if s.budget != nil {
		s.budget.RevokeTransfer(id)
	}
}
func validTransferID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
func serviceURL(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" {
		return nil, errors.New("approved HTTPS endpoint required")
	}
	return u, nil
}
func (s *ServiceTransfer) emit(id, state string) {
	if s.sink != nil {
		v, _ := json.Marshal(map[string]string{"event": "service_transfer", "id": id, "state": state})
		s.eventMu.Lock()
		defer s.eventMu.Unlock()
		s.sink.OnEvent(string(v))
	}
}
func (s *ServiceTransfer) Fetch(raw string, binder SocketBinder) (result error) {
	var r struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		URL    string `json:"https_url"`
		Output string `json:"output_file"`
		Auth   string `json:"device_auth_ref"`
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	claimed := false
	decodeErr := dec.Decode(&r)
	if s.budget != nil && validTransferID(r.ID) {
		defer func() {
			if !claimed {
				s.budget.discardPendingGrant(r.ID)
			}
		}()
	}
	if decodeErr != nil || !validTransferID(r.ID) || (r.Kind != "config" && r.Kind != "apk") || r.Output == "" || !filepath.IsAbs(r.Output) {
		return errors.New("invalid service transfer request")
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid service transfer request")
	}
	u, e := serviceURL(r.URL)
	if e != nil {
		return e
	}
	s.mu.Lock()
	trust := s.trust
	s.mu.Unlock()
	physical, ok := binder.(*budgetBinder)
	if s.budget == nil || !ok || physical.budget != s.budget || (physical.network != "wifi" && physical.network != "cell") || trust == nil || !trust.ApprovedURL(r.Kind, r.URL) {
		return errors.New("verified profile and physical network required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	s.mu.Lock()
	if s.canceled[r.ID] {
		s.mu.Unlock()
		return errors.New("service transfer canceled")
	}
	granted, e := s.budget.claimTransfer(r.ID, cancel)
	s.mu.Unlock()
	if e != nil {
		return e
	}
	claimed = true
	defer func() {
		s.budget.finishTransfer(r.ID)
		if result == nil {
			s.emit(r.ID, "completed")
		} else if physical.network == "cell" && !granted && !s.budget.CellAllowed() {
			s.emit(r.ID, "consent_required")
			result = trafficbudget.ErrBlocked
		} else {
			s.emit(r.ID, "failed")
		}
	}()
	if physical.network == "cell" && !granted && !s.budget.CellAllowed() {
		return trafficbudget.ErrBlocked
	}
	secret, e := trust.Credentials(r.Auth)
	if e != nil {
		return errors.New("device authentication unavailable")
	}
	var credentials struct {
		Token string `json:"token"`
		CA    string `json:"ca"`
	}
	if secret != "" && json.Unmarshal([]byte(secret), &credentials) != nil {
		return errors.New("invalid device authentication")
	}
	if r.Kind == "config" && (r.Auth == "" || credentials.Token == "") {
		return errors.New("configuration requires device authentication")
	}
	if strings.ContainsAny(credentials.Token, "\r\n") {
		return errors.New("invalid device authentication")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if credentials.CA != "" {
		config.RootCAs = x509.NewCertPool()
		if !config.RootCAs.AppendCertsFromPEM([]byte(credentials.CA)) {
			return errors.New("invalid profile CA")
		}
	}
	dialer := net.Dialer{Timeout: 15 * time.Second, Control: func(_, _ string, raw syscall.RawConn) error {
		var bindErr error
		e := raw.Control(func(fd uintptr) {
			if physical.SocketBinder != nil {
				bindErr = physical.SocketBinder.Bind(int64(fd))
			}
		})
		if e != nil {
			return e
		}
		return bindErr
	}}
	transport := &http.Transport{TLSClientConfig: config, DisableKeepAlives: true, DisableCompression: true, ForceAttemptHTTP2: false, ResponseHeaderTimeout: 30 * time.Second,
		DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			host, portText, e := net.SplitHostPort(address)
			if e != nil {
				return nil, e
			}
			port, e := strconv.Atoi(portText)
			if e != nil {
				return nil, e
			}
			numeric, e := trust.ResolveAddress(host, port)
			if e != nil {
				return nil, errors.New("physical DNS unavailable")
			}
			ip, p, e := net.SplitHostPort(numeric)
			if e != nil || net.ParseIP(ip) == nil || p != portText {
				return nil, errors.New("physical DNS returned invalid endpoint")
			}
			if physical.network == "cell" && !granted && !s.budget.CellAllowed() {
				return nil, trafficbudget.ErrBlocked
			}
			conn, e := dialer.DialContext(ctx, "tcp4", numeric)
			if e != nil {
				return nil, e
			}
			if physical.network == "cell" {
				class := trafficbudget.APK
				if r.Kind == "config" {
					class = trafficbudget.Config
				}
				return newServiceConn(conn, s.budget, class, granted, ctx), nil
			}
			return conn, nil
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		v, e := serviceURL(next.URL.String())
		if e != nil || !strings.EqualFold(v.Hostname(), u.Hostname()) || effectivePort(v) != effectivePort(u) || !trust.ApprovedURL(r.Kind, next.URL.String()) {
			return errors.New("service redirect rejected")
		}
		return nil
	}}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if e != nil {
		return errors.New("invalid service request")
	}
	if r.Kind == "config" && credentials.Token != "" {
		req.Header.Set("Authorization", "Bearer "+credentials.Token)
	}
	response, e := client.Do(req)
	if e != nil {
		return errors.New("service HTTPS request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("service response rejected")
	}
	limit := int64(256 << 20)
	if r.Kind == "config" {
		limit = 1 << 20
	}
	if response.ContentLength > limit {
		return errors.New("service response too large")
	}
	tmp, e := os.CreateTemp(filepath.Dir(r.Output), ".service-transfer-*")
	if e != nil {
		return errors.New("cannot create service output")
	}
	name := tmp.Name()
	defer os.Remove(name)
	defer tmp.Close()
	n, e := io.Copy(tmp, io.LimitReader(response.Body, limit+1))
	if e != nil || ctx.Err() != nil {
		return errors.New("service transfer interrupted")
	}
	if n > limit {
		return errors.New("service response too large")
	}
	if e = tmp.Sync(); e != nil {
		return errors.New("cannot save service output")
	}
	if e = tmp.Close(); e != nil {
		return errors.New("cannot save service output")
	}
	if e = s.publishTransfer(ctx, r.ID, name, r.Output); e != nil {
		return errors.New("cannot publish service output")
	}
	return nil
}
func effectivePort(u *url.URL) string {
	if u.Port() == "" {
		return "443"
	}
	return u.Port()
}

type serviceConn struct {
	net.Conn
	b          *TrafficBudget
	class      trafficbudget.Class
	granted    bool
	unregister func()
	stop       func() bool
}

func newServiceConn(c net.Conn, b *TrafficBudget, class trafficbudget.Class, granted bool, ctx context.Context) net.Conn {
	m := &serviceConn{Conn: c, b: b, class: class, granted: granted, unregister: func() {}}
	if !granted {
		m.unregister = b.meter.Register(func() { c.Close() })
	}
	m.stop = context.AfterFunc(ctx, func() { c.Close() })
	return m
}
func (m *serviceConn) Read(p []byte) (int, error) {
	if !m.granted && !m.b.CellAllowed() {
		return 0, trafficbudget.ErrBlocked
	}
	n, e := m.Conn.Read(p)
	m.b.meter.Receive(uint64(n), m.class)
	return n, e
}
func (m *serviceConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if m.granted {
		n, e := m.Conn.Write(p)
		m.b.meter.Receive(uint64(n), m.class)
		return n, e
	}
	ticket, ok := m.b.meter.Reserve(uint64(len(p)), m.class)
	if !ok {
		return 0, trafficbudget.ErrBlocked
	}
	n, e := m.Conn.Write(p)
	m.b.meter.Commit(ticket, uint64(n))
	return n, e
}
func (m *serviceConn) Close() error { m.stop(); m.unregister(); return m.Conn.Close() }

// Publication is the completion linearization point. Cancel wins if it marks
// this operation before this critical section; otherwise successful rename wins.
// The budget lock also serializes direct RevokeTransfer with publication.
func (s *ServiceTransfer) publishTransfer(ctx context.Context, id, source, output string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.budget.transferMu.Lock()
	defer s.budget.transferMu.Unlock()
	if s.canceled[id] || ctx.Err() != nil {
		return errors.New("service transfer canceled")
	}
	return os.Rename(source, output)
}
