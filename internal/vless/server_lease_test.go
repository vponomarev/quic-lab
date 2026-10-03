package vless

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestServerAdmissionLeaseFailsClosed(t *testing.T) {
	cert, key := fixtureCertificate(t)
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := l.Addr().String()
	l.Close()
	var deny atomic.Bool
	s, e := StartServer(context.Background(), ServerOptions{Listen: addr, Security: "tls", Certificate: cert, Key: key, Mode: "standalone", Clients: []ServerClient{{UUID: serverID}}, Admission: func(ctx context.Context, id string) (time.Duration, error) {
		if id != serverID || deny.Load() {
			return 0, errors.New("denied")
		}
		return 150 * time.Millisecond, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c, e := serverClient(t, addr, cert, serverID).DialContext(context.Background(), "tcp4", tcpEcho(t))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	exchange(t, c)
	deny.Store(true)
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, e = c.Read(make([]byte, 1)); e == nil {
		t.Fatal("lease survived")
	} else if n, ok := e.(net.Error); ok && n.Timeout() {
		t.Fatal("lease close timed out")
	}
}
