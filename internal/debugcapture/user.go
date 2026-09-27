package debugcapture

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func (m *Manager) UserCapture(user string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		v := s.Snapshot()
		if v.User == user && v.Kind == "user" && v.State == "running" {
			return s
		}
	}
	return nil
}
func (m *Manager) StartUser(owner, user, iface, ip string) (*Session, error) {
	if user == "" {
		return nil, errors.New("user required")
	}
	if iface != "" && (!regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,64}$`).MatchString(iface) || net.ParseIP(ip).To4() == nil) {
		return nil, errors.New("invalid AWG capture interface/address")
	}
	m.mu.Lock()
	active := 0
	for id, s := range m.sessions {
		v := s.Snapshot()
		if time.Now().After(v.Until.Add(5 * time.Minute)) {
			delete(m.sessions, id)
			continue
		}
		if v.State == "running" {
			active++
			if v.User == user {
				m.mu.Unlock()
				return nil, errors.New("захват этого пользователя уже включён")
			}
		}
	}
	if m.closing || active >= m.Config.Slots || len(m.sessions) >= 8 {
		m.mu.Unlock()
		return nil, errors.New("нет свободного слота захвата")
	}
	ctx, cancel := context.WithTimeout(m.parent, Lifetime)
	s := &Session{Info: Info{ID: random(), Owner: owner, Kind: "user", User: user, State: "running", Until: time.Now().Add(Lifetime)}, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), link: 101}
	_ = s.emit(section(101))
	m.sessions[s.ID] = s
	m.mu.Unlock()
	go func() { <-ctx.Done(); s.Stop("expired") }()
	time.AfterFunc(Lifetime+5*time.Minute, func() { m.mu.Lock(); delete(m.sessions, s.ID); m.mu.Unlock() })
	if iface != "" {
		if e := m.captureAWG(s, iface, ip); e != nil {
			s.Stop("AWG capture failed")
			return nil, e
		}
	}
	return s, nil
}
func (s *Session) packet(p []byte, comment string) {
	s.packetAt(p, comment, uint64(time.Now().UnixMicro()))
}
func (s *Session) packetAt(p []byte, comment string, now uint64) {
	if len(p) == 0 || len(p) > 65535 {
		return
	}
	b := make([]byte, 20+((len(p)+3)&^3))
	le.PutUint32(b[4:], uint32(now>>32))
	le.PutUint32(b[8:], uint32(now))
	le.PutUint32(b[12:], uint32(len(p)))
	le.PutUint32(b[16:], uint32(len(p)))
	copy(b[20:], p)
	option := make([]byte, 4+((len(comment)+3)&^3))
	le.PutUint16(option, 1)
	le.PutUint16(option[2:], uint16(len(comment)))
	copy(option[4:], comment)
	b = append(b, option...)
	b = append(b, 0, 0, 0, 0)
	_ = s.emit(block(6, b))
}
func (m *Manager) captureAWG(s *Session, iface, ip string) error {
	cmd := exec.CommandContext(s.ctx, m.Config.TCPDump, "--immediate-mode", "-n", "-p", "-i", iface, "-U", "-s", "0", "-w", "-", "ip and host "+ip)
	out, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	if e = cmd.Start(); e != nil {
		return e
	}
	ready := make(chan error, 1)
	go func() {
		var link uint32
		e := readPCAP(out, func(b []byte) error {
			if le.Uint32(b) == 0x0a0d0d0a {
				link = uint32(le.Uint16(b[36:]))
				return nil
			}
			size := int(le.Uint32(b[20:]))
			if 28+size > len(b)-4 {
				return errors.New("short packet")
			}
			p := b[28 : 28+size]
			offset := 0
			switch link {
			case 1:
				offset = 14
			case 113:
				offset = 16
			case 276:
				offset = 20
			case 101, 228:
			default:
				return errors.New("unsupported AWG link")
			}
			if len(p) < offset+20 {
				return nil
			}
			p = p[offset:]
			if p[0]>>4 != 4 {
				return nil
			}
			if net.IP(p[12:16]).String() != ip && net.IP(p[16:20]).String() != ip {
				return nil
			}
			s.packetAt(p, "AWG inner interface; user="+s.User, uint64(le.Uint32(b[12:]))<<32|uint64(le.Uint32(b[16:])))
			return nil
		}, ready)
		_ = cmd.Wait()
		if s.ctx.Err() == nil && e != nil {
			s.Stop("AWG capture ended")
		}
	}()
	select {
	case e := <-ready:
		return e
	case <-time.After(3 * time.Second):
		s.cancel()
		return errors.New("AWG capture timeout")
	}
}

var flowCounter atomic.Uint32

func (m *Manager) Wrap(user, network, address string, c net.Conn) net.Conn {
	if user == "" {
		return c
	}
	h, p, e := net.SplitHostPort(address)
	ip := net.ParseIP(h).To4()
	port, _ := strconv.Atoi(p)
	if e != nil || ip == nil || port < 1 || port > 65535 {
		return c
	}
	id := flowCounter.Add(1)
	return &capturedConn{Conn: c, manager: m, user: user, udp: strings.HasPrefix(network, "udp"), dst: ip, dport: uint16(port), src: net.IPv4(198, 18, byte(id>>16), byte(id>>8)).To4(), sport: uint16(1024 + id%64000), states: map[string]*flowState{}}
}

type flowState struct{ up, down uint32 }
type capturedConn struct {
	net.Conn
	manager      *Manager
	user         string
	udp          bool
	src, dst     net.IP
	sport, dport uint16
	mu           sync.Mutex
	states       map[string]*flowState
}

func (c *capturedConn) Read(p []byte) (int, error) {
	n, e := c.Conn.Read(p)
	if n > 0 {
		c.record(false, p[:n])
	}
	return n, e
}
func (c *capturedConn) Write(p []byte) (int, error) {
	n, e := c.Conn.Write(p)
	if n > 0 {
		c.record(true, p[:n])
	}
	return n, e
}
func (c *capturedConn) CloseWrite() error {
	if h, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return h.CloseWrite()
	}
	return nil
}
func (c *capturedConn) record(up bool, p []byte) {
	s := c.manager.UserCapture(c.user)
	if s == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[s.ID]
	if state == nil {
		c.states = map[string]*flowState{}
		state = &flowState{1000, 2000}
		c.states[s.ID] = state
		if !c.udp {
			c.emit(s, state, true, nil, 2)
			state.up++
			c.emit(s, state, false, nil, 18)
			state.down++
			c.emit(s, state, true, nil, 16)
		}
	}
	if c.udp {
		c.emit(s, state, up, p, 0)
		return
	}
	for len(p) > 0 {
		n := len(p)
		if n > 1400 {
			n = 1400
		}
		c.emit(s, state, up, p[:n], 24)
		if up {
			state.up += uint32(n)
		} else {
			state.down += uint32(n)
		}
		c.emit(s, state, !up, nil, 16)
		p = p[n:]
	}
}
func checksum(p []byte) uint16 {
	var s uint32
	for len(p) > 1 {
		s += uint32(binary.BigEndian.Uint16(p))
		p = p[2:]
	}
	if len(p) > 0 {
		s += uint32(p[0]) << 8
	}
	for s>>16 != 0 {
		s = (s & 65535) + (s >> 16)
	}
	return ^uint16(s)
}
func (c *capturedConn) emit(s *Session, state *flowState, up bool, data []byte, flags byte) {
	src, dst, sp, dp := c.src, c.dst, c.sport, c.dport
	seq, ack := state.up, state.down
	if !up {
		src, dst, sp, dp = dst, src, dp, sp
		seq, ack = ack, seq
	}
	proto := byte(6)
	header := 20
	if c.udp {
		proto = 17
		header = 8
	}
	if len(data)+header+20 > 65535 {
		return
	}
	b := make([]byte, 20+header+len(data))
	b[0] = 0x45
	binary.BigEndian.PutUint16(b[2:], uint16(len(b)))
	b[8] = 64
	b[9] = proto
	copy(b[12:], src)
	copy(b[16:], dst)
	binary.BigEndian.PutUint16(b[10:], checksum(b[:20]))
	payload := b[20:]
	binary.BigEndian.PutUint16(payload, sp)
	binary.BigEndian.PutUint16(payload[2:], dp)
	if c.udp {
		binary.BigEndian.PutUint16(payload[4:], uint16(len(payload)))
	} else {
		binary.BigEndian.PutUint32(payload[4:], seq)
		if flags&16 != 0 {
			binary.BigEndian.PutUint32(payload[8:], ack)
		}
		payload[12] = 0x50
		payload[13] = flags
		binary.BigEndian.PutUint16(payload[14:], 65535)
	}
	copy(payload[header:], data)
	pseudo := make([]byte, 12+len(payload))
	copy(pseudo, src)
	copy(pseudo[4:], dst)
	pseudo[9] = proto
	binary.BigEndian.PutUint16(pseudo[10:], uint16(len(payload)))
	copy(pseudo[12:], payload)
	sum := checksum(pseudo)
	if c.udp {
		if sum == 0 {
			sum = 65535
		}
		binary.BigEndian.PutUint16(payload[6:], sum)
	} else {
		binary.BigEndian.PutUint16(payload[16:], sum)
	}
	s.packet(b, fmt.Sprintf("Reconstructed proxy flow; synthetic TCP/IP headers, not wire timing; user=%s", c.user))
}

var _ io.ReadWriteCloser = (*capturedConn)(nil)

func (m *Manager) Mode(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[id]; s != nil && s.User != "" {
		return "inner"
	}
	return "transport"
}
func (m *Manager) StopUser(user string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.User == user {
			s.Stop("user revoked")
		}
	}
}
