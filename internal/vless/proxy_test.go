package vless

import (
	"context"
	proxyproto "github.com/pires/go-proxyproto"
	"net"
	"testing"
	"time"
)

type proxyFixtureDialer struct{ net.Dialer }

func (d *proxyFixtureDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	c, err := d.Dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	h := proxyproto.HeaderProxyFromAddrs(2, &net.TCPAddr{IP: net.ParseIP("198.51.100.17"), Port: 32109}, &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 443})
	if _, err = h.WriteTo(c); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func TestServerProxyProtocolPreservesAuthenticatedPeer(t *testing.T) {
	cert, key := fixtureCertificate(t)
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	seen := make(chan string, 8)
	s, err := StartServer(context.Background(), ServerOptions{Listen: addr, Security: "tls", Mode: "standalone", Certificate: cert, Key: key, Clients: []ServerClient{{UUID: serverID}}, AcceptProxyProtocol: true, Admission: func(ctx context.Context, id string) (time.Duration, error) {
		select {
		case seen <- AuthenticatedPeer(ctx):
		default:
		}
		return 2 * time.Second, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := New(context.Background(), Config{Endpoint: addr, UUID: serverID, Security: "tls", ServerName: "fixture.invalid", RootPEM: cert}, &proxyFixtureDialer{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	conn, err := c.DialContext(context.Background(), "tcp4", tcpEcho(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	exchange(t, conn)
	select {
	case peer := <-seen:
		if peer != "198.51.100.17:32109" {
			t.Fatalf("peer=%q", peer)
		}
	case <-time.After(time.Second):
		t.Fatal("no admitted peer")
	}
	s.Revoke(serverID)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("revoked PROXY connection remains open")
	} else if n, ok := err.(net.Error); ok && n.Timeout() {
		t.Fatal("revocation timeout")
	}
}

type proxyOptionalDialer struct{ proxy bool }

func (d *proxyOptionalDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d.proxy {
		return (&proxyFixtureDialer{}).DialContext(ctx, network, address)
	}
	return (&net.Dialer{}).DialContext(ctx, network, address)
}
