package debugcapture

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func userManager(t *testing.T) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &Manager{Config: Config{Slots: 2}, parent: ctx, sessions: map[string]*Session{}}
}
func userPackets(t *testing.T, s *Session) [][]byte {
	t.Helper()
	bs, _ := s.Blocks(0)
	var out [][]byte
	for _, b := range bs {
		if le.Uint32(b) != 6 {
			continue
		}
		n := int(le.Uint32(b[20:]))
		p := b[28 : 28+n]
		if checksum(p[:20]) != 0 {
			t.Fatal("invalid IP checksum")
		}
		proto := p[9]
		pseudo := make([]byte, 12+len(p)-20)
		copy(pseudo, p[12:20])
		pseudo[9] = proto
		binary.BigEndian.PutUint16(pseudo[10:], uint16(len(p)-20))
		copy(pseudo[12:], p[20:])
		if checksum(pseudo) != 0 {
			t.Fatal("invalid transport checksum")
		}
		out = append(out, p)
	}
	return out
}
func TestUserCaptureIsolationLifecycle(t *testing.T) {
	m := userManager(t)
	a, e := m.StartUser("teacher-a", "alice", "", "")
	if e != nil {
		t.Fatal(e)
	}
	defer a.Stop("test")
	b, e := m.StartUser("teacher-b", "bob", "", "")
	if e != nil {
		t.Fatal(e)
	}
	defer b.Stop("test")
	if _, e = m.StartUser("teacher-c", "alice", "", ""); e == nil {
		t.Fatal("duplicate user")
	}
	if _, e = m.StartUser("teacher-c", "charlie", "", ""); e == nil {
		t.Fatal("slots ignored")
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	c := m.Wrap("alice", "tcp4", "203.0.113.1:80", left).(*capturedConn)
	c.record(true, []byte("GET /alice HTTP/1.1\r\nHost: test\r\n\r\n"))
	c.record(false, []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK"))
	packets := userPackets(t, a)
	if len(packets) != 7 {
		t.Fatalf("packets: %d", len(packets))
	}
	if len(userPackets(t, b)) != 0 {
		t.Fatal("cross-user traffic")
	}
	router := &Router{}
	router.Set(m)
	router.Writer(0, true).Write([]byte("test-secret\n"))
	if len(a.Keys()) != 0 || len(b.Keys()) != 0 {
		t.Fatal("outer TLS secret leaked")
	}
	if m.Get(a.ID, "teacher-b") != nil {
		t.Fatal("cross-teacher access")
	}
	before := a.Snapshot().Bytes
	m.StopUser("alice")
	c.record(true, []byte("after stop"))
	if a.Snapshot().Bytes != before {
		t.Fatal("recorded after stop")
	}
	done := make(chan error, 1)
	go func() { _, e := right.Read(make([]byte, 32)); done <- e }()
	if _, e = c.Write([]byte("still connected")); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if m.UserCapture("bob") == nil {
		t.Fatal("stopped other user")
	}
}
func TestUserUDPBoundaries(t *testing.T) {
	m := userManager(t)
	s, e := m.StartUser("teacher", "alice", "", "")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Stop("test")
	c := m.Wrap("alice", "udp4", "203.0.113.2:53", nil).(*capturedConn)
	c.record(true, []byte("query-one"))
	c.record(true, []byte("query-two"))
	c.record(false, []byte("answer"))
	ps := userPackets(t, s)
	if len(ps) != 3 || !bytes.Equal(ps[0][28:], []byte("query-one")) || !bytes.Equal(ps[1][28:], []byte("query-two")) {
		t.Fatal("UDP boundaries changed")
	}
	if got := net.IP(ps[2][12:16]).String(); got != "203.0.113.2" {
		t.Fatal("response direction", got)
	}
}
func TestUserAWGInterfaceFilter(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("Linux root tcpdump integration")
	}
	path, e := exec.LookPath("tcpdump")
	if e != nil {
		t.Skip(e)
	}
	m := userManager(t)
	m.Config.TCPDump = path
	s, e := m.StartUser("teacher", "alice", "lo", "127.0.0.2")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Stop("test")
	for _, ip := range []string{"127.0.0.2", "127.0.0.3"} {
		l, e := net.ListenPacket("udp4", ip+":0")
		if e != nil {
			t.Fatal(e)
		}
		c, e := net.Dial("udp4", l.LocalAddr().String())
		if e != nil {
			t.Fatal(e)
		}
		c.Write([]byte("marker-" + ip))
		l.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 64)
		n, peer, e := l.ReadFrom(buf)
		if e != nil {
			t.Fatal(e)
		}
		l.WriteTo(buf[:n], peer)
		c.SetReadDeadline(time.Now().Add(time.Second))
		c.Read(buf)
		c.Close()
		l.Close()
	}
	deadline := time.Now().Add(3 * time.Second)
	for s.Snapshot().Bytes < 200 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	s.Stop("test")
	bs, _ := s.Blocks(0)
	all := bytes.Join(bs, nil)
	if !bytes.Contains(all, []byte("marker-127.0.0.2")) || bytes.Contains(all, []byte("marker-127.0.0.3")) {
		t.Fatal("interface user filter failed")
	}
}
