package vless

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
)

func TestServerExitReceivesAuthenticatedIdentityForTCPAndUDP(t *testing.T) {
	cert, key := fixtureCertificate(t)
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	tcp, udp := tcpEcho(t), udpEcho(t)
	seen := map[string]int{}
	var mu sync.Mutex
	s, err := StartServer(context.Background(), ServerOptions{Listen: addr, Security: "tls", Mode: "standalone", Certificate: cert, Key: key, Clients: []ServerClient{{UUID: serverID}, {UUID: otherServerID}}, ExitDial: func(ctx context.Context, id, network, destination string) (net.Conn, error) {
		if id != serverID && id != otherServerID {
			return nil, errors.New("untrusted identity")
		}
		mu.Lock()
		seen[id+"/"+network]++
		mu.Unlock()
		if network == "tcp4" {
			return (&net.Dialer{}).DialContext(ctx, network, tcp)
		}
		return (&net.Dialer{}).DialContext(ctx, network, udp)
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{serverID, otherServerID} {
		for _, network := range []string{"tcp4", "udp4"} {
			client := serverClient(t, addr, cert, id)
			c, err := client.DialContext(context.Background(), network, "192.0.2.1:7777")
			if err != nil {
				t.Fatal(err)
			}
			exchange(t, c)
			c.Close()
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for _, id := range []string{serverID, otherServerID} {
		for _, network := range []string{"tcp4", "udp4"} {
			if seen[id+"/"+network] == 0 {
				t.Fatal("lost identity", id, network)
			}
		}
	}
}
