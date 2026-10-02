package debugcapture

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const MaxBytes = 32 << 20
const Lifetime = 10 * time.Minute

type Config struct {
	Interface   string             `json:"interface"`
	FirstPort   int                `json:"-"` // integration-test listeners only
	Termination map[string][2]bool `json:"-"`
	Ports       map[string][2]int  `json:"-"` // existing UDP/TCP endpoints from server config
	Slots       int                `json:"slots"`
	TCPDump     string             `json:"tcpdump"`
}

func (c Config) Validate() error {
	if !regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,64}$`).MatchString(c.Interface) || (c.FirstPort != 0 && c.FirstPort < 1024) || c.Slots < 1 || c.Slots > 4 || c.FirstPort+c.Slots > 65536 || !filepath.IsAbs(c.TCPDump) {
		return errors.New("capture requires interface, absolute tcpdump path, slots 1..4")
	}
	return nil
}

type Info struct {
	ID, Owner, Kind, User, State string
	Port                         int
	TCPPort                      int
	Bytes                        int
	Until                        time.Time
}
type Session struct {
	mu sync.Mutex
	Info
	ctx             context.Context
	cancel          context.CancelFunc
	blocks          [][]byte
	times           []time.Time
	keyLines        []byte
	uploadToken     string
	link            uint32
	wake            chan struct{}
	ticket, token   string
	ticketUntil     time.Time
	enrollment      string
	enrollmentUntil time.Time
	connected       bool
}

func (s *Session) Context() context.Context { return s.ctx }
func (s *Session) Snapshot() Info           { s.mu.Lock(); defer s.mu.Unlock(); return s.Info }
func (s *Session) emit(b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State != "running" {
		return io.EOF
	}
	if s.Bytes+len(b) > MaxBytes {
		s.State = "capture limit (32 MiB)"
		s.cancel()
		select {
		case s.wake <- struct{}{}:
		default:
		}
		return io.EOF
	}
	s.blocks = append(s.blocks, b)
	s.times = append(s.times, time.Now())
	s.Bytes += len(b)
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}

// Write receives server TLS secrets only for selected active capture listeners.
func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	if s.State != "running" || len(s.keyLines)+len(p) > 1<<20 {
		s.mu.Unlock()
		return len(p), nil
	}
	s.keyLines = append(s.keyLines, p...)
	s.mu.Unlock()
	_ = s.emit(secrets(p))
	return len(p), nil
}
func (s *Session) Stop(reason string) {
	s.mu.Lock()
	if s.State == "running" {
		s.State = reason
	}
	s.cancel()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	s.mu.Unlock()
}
func (s *Session) Blocks(from int) ([][]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.blocks[from:]...), s.State != "running"
}
func (s *Session) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return nil
	case <-s.wake:
		return nil
	case <-time.After(15 * time.Second):
		return nil
	}
}
func random() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}

type Factory func(*Session) (func(), error)
type Manager struct {
	mu       sync.Mutex
	Config   Config
	sessions map[string]*Session
	Factory  Factory
	closing  bool
	parent   context.Context
	excluded map[string]int
}

func New(ctx context.Context, c Config, f Factory) (*Manager, error) {
	if e := c.Validate(); e != nil {
		return nil, e
	}
	m := &Manager{Config: c, sessions: map[string]*Session{}, Factory: f, parent: ctx, excluded: map[string]int{}}
	go func() {
		<-ctx.Done()
		m.mu.Lock()
		m.closing = true
		for _, s := range m.sessions {
			s.Stop("server stopped")
		}
		m.mu.Unlock()
	}()
	return m, nil
}
func (m *Manager) Start(owner, kind, user string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return nil, errors.New("server stopping")
	}
	if user != "" {
		return nil, errors.New("use dedicated per-user capture")
	}
	if kind != "echo" && kind != "vpn" {
		return nil, errors.New("unknown capture mode")
	}
	ports := map[int]bool{}
	for id, s := range m.sessions {
		v := s.Snapshot()
		if time.Now().After(v.Until.Add(5 * time.Minute)) {
			delete(m.sessions, id)
		} else if v.State == "running" {
			ports[v.Port] = true
		}
	}
	if len(m.sessions) >= 8 {
		return nil, errors.New("capture retention limit; wait for expiry")
	}
	port := 0
	tcpPort := 0
	for p := m.Config.FirstPort; p < m.Config.FirstPort+m.Config.Slots; p++ {
		if !ports[p] {
			port = p
			break
		}
	}
	if m.Config.FirstPort == 0 {
		pair := m.Config.Ports[kind]
		port = pair[0]
		tcpPort = pair[1]
		active := 0
		for _, v := range m.sessions {
			if v.Snapshot().State == "running" {
				active++
			}
		}
		if active >= m.Config.Slots {
			return nil, errors.New("all capture slots are busy")
		}
	} else {
		tcpPort = port
	}
	if port == 0 || tcpPort == 0 {
		return nil, errors.New("all debug listeners are busy")
	}
	ctx, cancel := context.WithTimeout(m.parent, Lifetime)
	s := &Session{Info: Info{ID: random(), Owner: owner, Kind: kind, User: user, State: "running", Port: port, TCPPort: tcpPort, Until: time.Now().Add(Lifetime)}, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), uploadToken: random()}
	// No shell, user-supplied capture command, broad interface filter or privilege escalation.
	cmd := exec.CommandContext(ctx, m.Config.TCPDump, "--immediate-mode", "-n", "-p", "-i", m.Config.Interface, "-U", "-s", "0", "-w", "-", "(tcp port "+strconv.Itoa(port)+" or udp port "+strconv.Itoa(port)+")")
	filter, e := localFilter(port, tcpPort)
	if e != nil {
		cancel()
		return nil, e
	}
	cmd.Args[len(cmd.Args)-1] = filter
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		cancel()
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		cancel()
		return nil, e
	}
	ready := make(chan error, 1)
	go func() {
		e := readPCAP(stdout, func(b []byte) error {
			if le.Uint32(b) == 0x0a0d0d0a {
				s.link = uint32(le.Uint16(b[36:]))
			}
			if le.Uint32(b) == 6 && m.isExcluded(b, s.link) {
				return nil
			}
			return s.emit(b)
		}, ready)
		_ = cmd.Wait()
		if ctx.Err() == nil {
			s.Stop(fmt.Sprintf("capture ended: %v", e))
		}
	}()
	select {
	case e = <-ready:
	case <-time.After(4 * time.Second):
		e = errors.New("tcpdump did not start; check CAP_NET_RAW and interface")
	}
	if e != nil {
		s.Stop("capture failed")
		return nil, fmt.Errorf("tcpdump startup failed; check interface and CAP_NET_RAW: %w", e)
	}
	cleanup := func() {}
	if m.Factory != nil {
		cleanup, e = m.Factory(s)
	}
	if e != nil {
		s.Stop("listener failed")
		return nil, e
	}
	m.sessions[s.ID] = s
	time.AfterFunc(time.Until(s.Until.Add(5*time.Minute)), func() {
		m.mu.Lock()
		delete(m.sessions, s.ID)
		m.mu.Unlock()
	})
	go func() { <-ctx.Done(); cleanup(); s.Stop("expired") }()
	return s, nil
}
func (m *Manager) Get(id, owner string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil || s.Owner != owner || time.Now().After(s.Until.Add(5*time.Minute)) {
		return nil
	}
	return s
}
func (m *Manager) List(owner string) []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Info
	for _, s := range m.sessions {
		if s.Owner == owner && time.Now().Before(s.Until.Add(5*time.Minute)) {
			out = append(out, s.Snapshot())
		}
	}
	return out
}
func (m *Manager) StopOwner(owner string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.Owner == owner {
			s.Stop("logged out")
			s.mu.Lock()
			s.ticket = ""
			s.token = ""
			s.mu.Unlock()
		}
	}
}
func (s *Session) Ticket() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State != "running" || s.connected {
		return "", errors.New("capture stopped or already attached")
	}
	s.ticket = random()
	s.ticketUntil = time.Now().Add(time.Minute)
	return s.ticket, nil
}
func (m *Manager) Redeem(id, ticket string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		return "", errors.New("unknown capture")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ticket == "" || s.ticket != ticket || time.Now().After(s.ticketUntil) || s.State != "running" {
		return "", errors.New("expired launch ticket")
	}
	s.ticket = ""
	s.token = random()
	return s.token, nil
}
func (m *Manager) Attach(id, token string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" || s.token != token || s.connected || s.State != "running" {
		return nil
	}
	s.token = ""
	s.connected = true
	return s
}

func (m *Manager) Port(id string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[id]; s != nil {
		return s.Port
	}
	return 0
}

// Match the local endpoint, not an unrelated remote peer that happens to use
// the same source port on a normal production listener.
func localFilter(udpPort, tcpPort int) (string, error) {
	addrs, e := net.InterfaceAddrs()
	if e != nil {
		return "", e
	}
	var to, from []string
	for _, addr := range addrs {
		ip, _, e := net.ParseCIDR(addr.String())
		if e != nil || ip.To4() == nil {
			continue
		}
		to = append(to, "dst host "+ip.String())
		from = append(from, "src host "+ip.String())
	}
	if len(to) == 0 {
		return "", errors.New("no local IPv4 capture addresses")
	}
	return fmt.Sprintf("((udp and dst port %d) or (tcp and dst port %d)) and (%s) or (((udp and src port %d) or (tcp and src port %d)) and (%s))", udpPort, tcpPort, strings.Join(to, " or "), udpPort, tcpPort, strings.Join(from, " or ")), nil
}

func (s *Session) UploadToken() string { return s.uploadToken }
func (s *Session) Keys() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.keyLines...)
}
func (s *Session) LiveBlocks(from int) ([][]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	end := len(s.blocks)
	if s.State == "running" {
		for end > from && time.Since(s.times[end-1]) < 2*time.Second {
			end--
		}
	}
	return append([][]byte(nil), s.blocks[from:end]...), s.State != "running" && end == len(s.blocks)
}
func (s *Session) SecretBlock() []byte { return secrets(s.Keys()) }
func (m *Manager) Upload(id, token string, lines []byte) bool {
	m.mu.Lock()
	s := m.sessions[id]
	m.mu.Unlock()
	if s == nil || token == "" || token != s.uploadToken || s.Snapshot().State != "running" || len(lines) > 8192 {
		return false
	}
	pattern := regexp.MustCompile(`^(CLIENT_RANDOM|CLIENT_HANDSHAKE_TRAFFIC_SECRET|SERVER_HANDSHAKE_TRAFFIC_SECRET|CLIENT_TRAFFIC_SECRET_0|SERVER_TRAFFIC_SECRET_0|EXPORTER_SECRET) [a-fA-F0-9]{64} [a-fA-F0-9]{64,96}$`)
	rows := strings.Split(strings.TrimSpace(string(lines)), "\n")
	if len(rows) == 0 {
		return false
	}
	for _, line := range rows {
		if !pattern.MatchString(line) {
			return false
		}
	}
	_, _ = s.Write(append([]byte(strings.Join(rows, "\n")), '\n'))
	return true
}
func (m *Manager) Exclude(peer string) func() {
	m.mu.Lock()
	m.excluded[peer]++
	m.mu.Unlock()
	return func() {
		time.AfterFunc(3*time.Second, func() {
			m.mu.Lock()
			m.excluded[peer]--
			if m.excluded[peer] <= 0 {
				delete(m.excluded, peer)
			}
			m.mu.Unlock()
		})
	}
}
func (m *Manager) isExcluded(b []byte, link uint32) bool {
	p := b[28 : len(b)-4]
	offset := 0
	switch link {
	case 1:
		offset = 14
	case 113:
		offset = 16
	case 276:
		offset = 20
	case 101:
		offset = 0
	default:
		return true
	}
	if len(p) < offset+20 {
		return true
	}
	p = p[offset:]
	if p[0]>>4 != 4 {
		return true
	}
	hlen := int(p[0]&15) * 4
	if hlen < 20 || len(p) < hlen+4 {
		return true
	}
	src := net.JoinHostPort(net.IP(p[12:16]).String(), strconv.Itoa(int(p[hlen])<<8|int(p[hlen+1])))
	dst := net.JoinHostPort(net.IP(p[16:20]).String(), strconv.Itoa(int(p[hlen+2])<<8|int(p[hlen+3])))
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.excluded[src] > 0 || m.excluded[dst] > 0
}

func (m *Manager) TCPPort(id string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[id]; s != nil {
		return s.TCPPort
	}
	return 0
}

func (s *Session) SecretBlockSince(n int) []byte {
	p := s.Keys()
	if n > len(p) {
		n = len(p)
	}
	return secrets(p[n:])
}

// Enrollment only grants upload configuration; never capture read access.
func (s *Session) Enrollment() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State != "running" || time.Now().After(s.Until) {
		return "", errors.New("capture stopped")
	}
	s.enrollment = random()[:48]
	s.enrollmentUntil = time.Now().Add(2 * time.Minute)
	return s.enrollment, nil
}
func (m *Manager) Enroll(token string) *Session {
	if !regexp.MustCompile(`^[a-f0-9]{48}$`).MatchString(token) {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		s.mu.Lock()
		ok := s.enrollment == token && s.State == "running" && time.Now().Before(s.enrollmentUntil) && time.Now().Before(s.Until)
		if ok {
			s.enrollment = ""
		}
		s.mu.Unlock()
		if ok {
			return s
		}
	}
	return nil
}

// CaptureIncludesVPN reports whether a global capture can include a multiplexed
// VPN listener. Compare UDP and TCP independently: equal numeric ports on
// different transport protocols do not overlap.
func (m *Manager) CaptureIncludesVPN(kind string) bool {
	if kind == "vpn" {
		return true
	}
	pair := m.Config.Ports[kind]
	vpn := m.Config.Ports["vpn"]
	return (pair[0] != 0 && pair[0] == vpn[0]) || (pair[1] != 0 && pair[1] == vpn[1])
}

// StopMultiplexedScope stops capture only. Existing VPN transports and all
// capture records remain available to their authenticated administrator.
func (m *Manager) StopMultiplexedScope(user string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		info := s.Snapshot()
		if (info.Kind == "user" && info.User == user) || (info.User == "" && m.CaptureIncludesVPN(info.Kind)) {
			s.Stop("capture stopped: target entered multiplexed bond")
		}
	}
}
