package vless

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

const serverID = "11111111-1111-4111-8111-111111111111"
const otherServerID = "22222222-2222-4222-8222-222222222222"

func serverFixture(t *testing.T, mode, target string) (*Server, string, []byte) {
	t.Helper()
	cert, key := fixtureCertificate(t)
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := l.Addr().String()
	l.Close()
	s, e := StartServer(context.Background(), ServerOptions{Listen: addr, Security: "tls", Certificate: cert, Key: key, Mode: mode, DemuxEndpoint: target, Clients: []ServerClient{{UUID: serverID}, {UUID: otherServerID}}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, addr, cert
}
func serverClient(t *testing.T, addr string, cert []byte, id string) Client {
	t.Helper()
	c, e := New(context.Background(), Config{Endpoint: addr, UUID: id, Security: "tls", ServerName: "fixture.invalid", RootPEM: cert}, &net.Dialer{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func tcpEcho(t *testing.T) string {
	t.Helper()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	return l.Addr().String()
}
func exchange(t *testing.T, c net.Conn) {
	t.Helper()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	p := []byte("server round trip")
	if _, e := c.Write(p); e != nil {
		t.Fatal(e)
	}
	b := make([]byte, len(p))
	if _, e := io.ReadFull(c, b); e != nil || !bytes.Equal(b, p) {
		t.Fatalf("round trip failed: %v", e)
	}
}
func TestServerConcurrentDeviceConnections(t *testing.T) {
	for _, n := range []int{2, 5} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			s, addr, cert := serverFixture(t, "standalone", "")
			target := tcpEcho(t)
			a := serverClient(t, addr, cert, serverID)
			b := serverClient(t, addr, cert, otherServerID)
			var conns []net.Conn
			for i := 0; i < n; i++ {
				c, e := a.DialContext(context.Background(), "tcp4", target)
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { c.Close() })
				exchange(t, c)
				conns = append(conns, c)
			}
			other, e := b.DialContext(context.Background(), "tcp4", target)
			if e != nil {
				t.Fatal(e)
			}
			defer other.Close()
			exchange(t, other)
			conns[0].Close()
			for _, c := range conns[1:] {
				exchange(t, c)
			}
			s.Revoke(serverID)
			for _, c := range conns[1:] {
				c.SetReadDeadline(time.Now().Add(time.Second))
				if _, e := c.Read(make([]byte, 1)); e == nil {
					t.Fatal("revoked flow open")
				} else if n, ok := e.(net.Error); ok && n.Timeout() {
					t.Fatal("revocation timed out")
				}
			}
			exchange(t, other)
			late, e := a.DialContext(context.Background(), "tcp4", target)
			if e == nil {
				defer late.Close()
				late.SetDeadline(time.Now().Add(time.Second))
				late.Write([]byte("denied"))
				if _, e = late.Read(make([]byte, 6)); e == nil {
					t.Fatal("revoked identity admitted")
				}
			}
		})
	}
}
func TestServerUDPRevoke(t *testing.T) {
	s, addr, cert := serverFixture(t, "standalone", "")
	p, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	go func() {
		b := make([]byte, 65535)
		for {
			n, a, e := p.ReadFrom(b)
			if e != nil {
				return
			}
			p.WriteTo(b[:n], a)
		}
	}()
	c, e := serverClient(t, addr, cert, serverID).DialContext(context.Background(), "udp4", p.LocalAddr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	exchange(t, c)
	s.Revoke(serverID)
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, e = c.Read(make([]byte, 1)); e == nil {
		t.Fatal("UDP still open")
	} else if n, ok := e.(net.Error); ok && n.Timeout() {
		t.Fatal("UDP revocation timed out")
	}
}
func TestDemuxOnlyPolicy(t *testing.T) {
	target := tcpEcho(t)
	_, addr, cert := serverFixture(t, "demux-only", target)
	a := serverClient(t, addr, cert, serverID)
	c, e := a.DialContext(context.Background(), "tcp4", target)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	exchange(t, c)
	for _, network := range []string{"tcp4", "udp4"} {
		denied := tcpEcho(t)
		c, e := a.DialContext(context.Background(), network, denied)
		if e != nil {
			continue
		}
		c.SetDeadline(time.Now().Add(time.Second))
		c.Write([]byte("denied"))
		_, e = c.Read(make([]byte, 64))
		c.Close()
		if e == nil {
			t.Fatal("demux-only escaped")
		}
	}
}
