package vless

import (
	"context"
	"fmt"
	"testing"
)

func TestServerUpdateClientsKeepsOtherDevice(t *testing.T) {
	s, addr, cert := serverFixture(t, "standalone", "")
	target := tcpEcho(t)
	c, e := serverClient(t, addr, cert, otherServerID).DialContext(context.Background(), "tcp4", target)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	exchange(t, c)
	third := "33333333-3333-4333-8333-333333333333"
	if e := s.SetClients(context.Background(), []ServerClient{{UUID: otherServerID}, {UUID: third}}); e != nil {
		t.Fatal(e)
	}
	exchange(t, c)
	n, e := serverClient(t, addr, cert, third).DialContext(context.Background(), "tcp4", target)
	if e != nil {
		t.Fatal(e)
	}
	defer n.Close()
	exchange(t, n)
	if e := s.SetClients(context.Background(), []ServerClient{{UUID: serverID}, {UUID: otherServerID}}); e == nil {
		t.Fatal("removed UUID silently resurrected")
	}
	exchange(t, c)
}

func TestServerConfiguredCapacityAndUpdate(t *testing.T) {
	s, _, _ := serverFixture(t, "standalone", "")
	clients := []ServerClient{{UUID: serverID}, {UUID: otherServerID}}
	for i := 0; i < 510; i++ {
		clients = append(clients, ServerClient{UUID: fmt.Sprintf("%08x-1111-4111-8111-111111111111", i+1)})
	}
	if e := s.SetClients(context.Background(), clients); e != nil {
		t.Fatal("512 configured accounts rejected")
	}
}
