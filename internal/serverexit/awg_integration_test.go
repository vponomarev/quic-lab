//go:build linux

package serverexit

import (
	"context"
	"io"
	"net"
	"net/netip"
	"os"
	"quiclab/internal/awg/netstack"
	"quiclab/internal/awgserver"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"golang.org/x/net/dns/dnsmessage"
)

// Independent encrypted peers deliberately use the SAME private addresses,
// ports and DNS server. Neither provider can borrow the other's stack.
func realExit(t *testing.T, name string, answer [4]byte) (Definition, func()) {
	t.Helper()
	// The endpoint is still this test host. A non-loopback local address lets
	// the peer stack answer an encrypted endpoint probe without martian routing.
	host := netip.Addr{}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr().Is4() && prefix.Addr().IsGlobalUnicast() && !prefix.Addr().IsLoopback() {
			host = prefix.Addr()
			break
		}
	}
	if !host.IsValid() {
		t.Skip("real exit probe requires a local non-loopback IPv4 address")
	}
	tun, network, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.55.0.1"), host}, nil, 1280)
	if err != nil {
		t.Fatal(err)
	}
	native := device.NewDevice(tun, conn.NewStdNetBind(), &device.Logger{Verbosef: func(string, ...any) {}, Errorf: func(string, ...any) {}})
	identity, _ := awgserver.NewIdentity()
	peer, _ := awgserver.NewPeer("10.55.0.2")
	cfg := awgserver.Config{Endpoint: "127.0.0.1:0", Address: "10.55.0.1/24", MTU: 1280, DNS: "10.55.0.1", AllowedIPs: []string{"0.0.0.0/0"}}
	w := awgserver.Worker{Device: native, Config: cfg}
	if err = w.Apply(awgserver.State{AWG: identity, Users: map[string]awgserver.User{"client": {ID: "client", Protocols: []string{"awg"}, Expires: time.Now().Add(time.Hour), AWG: peer}}}); err != nil {
		t.Fatal(err)
	}
	if err = native.Up(); err != nil {
		t.Fatal(err)
	}
	ipc, _ := native.IpcGet()
	port := ""
	for _, line := range strings.Split(ipc, "\n") {
		if strings.HasPrefix(line, "listen_port=") {
			port = strings.TrimPrefix(line, "listen_port=")
		}
	}
	tcp, err := network.ListenTCPAddrPort(netip.MustParseAddrPort("10.55.0.1:7777"))
	if err != nil {
		t.Fatal(err)
	}
	udp, err := network.ListenUDPAddrPort(netip.MustParseAddrPort("10.55.0.1:7777"))
	if err != nil {
		t.Fatal(err)
	}
	dns, err := network.ListenUDPAddrPort(netip.MustParseAddrPort("10.55.0.1:53"))
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(func() { tcp.Close(); udp.Close(); dns.Close(); native.Close() }) }
	t.Cleanup(stop)
	go func() {
		for {
			c, err := tcp.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				var b [1]byte
				if _, err := io.ReadFull(c, b[:]); err == nil {
					c.Write([]byte(name))
				}
			}()
		}
	}()
	go func() {
		b := make([]byte, 2048)
		for {
			_, from, err := udp.ReadFrom(b)
			if err != nil {
				return
			}
			udp.WriteTo([]byte(name), from)
		}
	}()
	go func() {
		b := make([]byte, 2048)
		for {
			n, from, err := dns.ReadFrom(b)
			if err != nil {
				return
			}
			var q dnsmessage.Message
			if q.Unpack(b[:n]) != nil {
				continue
			}
			reply := dnsmessage.Message{Header: dnsmessage.Header{ID: q.Header.ID, Response: true, RecursionDesired: q.Header.RecursionDesired, RecursionAvailable: true}, Questions: q.Questions}
			for _, question := range q.Questions {
				if question.Type == dnsmessage.TypeA {
					reply.Answers = append(reply.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 1}, Body: &dnsmessage.AResource{A: answer}})
				}
			}
			raw, _ := reply.Pack()
			dns.WriteTo(raw, from)
		}
	}()
	raw := strings.Replace(cfg.Client(*identity, *peer), "Endpoint = 127.0.0.1:0", "Endpoint = "+net.JoinHostPort(host.String(), port), 1)
	return Definition{ID: name, Name: name, Protocol: "awg", AWGConfig: raw}, stop
}
func TestTwoRealAWGExitsTCPUDPDNSAndOutage(t *testing.T) {
	west, stopWest := realExit(t, "west", [4]byte{198, 51, 100, 10})
	east, _ := realExit(t, "east", [4]byte{198, 51, 100, 11})
	catalog, err := NewCatalog([]Definition{west, east})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	r, err := StartOwned(context.Background(), catalog, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	exchange := func(t *testing.T, p Provider, kind, want string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		c, err := p.DialContext(ctx, kind, "10.55.0.1:7777")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		c.Write([]byte("x"))
		buf := make([]byte, len(want))
		if _, err = io.ReadFull(c, buf); err != nil || string(buf) != want {
			t.Fatal(kind, string(buf), err)
		}
	}
	for _, id := range []string{"west", "east"} {
		p, _ := r.Provider(id)
		for _, kind := range []string{"tcp4", "udp4"} {
			exchange(t, p, kind, id)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		ips, err := p.Resolver().LookupIP(ctx, "ip4", "exit.test")
		want := "198.51.100.10"
		if id == "east" {
			want = "198.51.100.11"
		}
		if err != nil || len(ips) != 1 || ips[0].String() != want {
			t.Fatal("DNS crossed exit", ips, err)
		}
		if err = p.Probe(ctx); err != nil {
			t.Fatal("ICMP escaped exit", id, err)
		}
		cancel()
	}
	stopWest()
	p, _ := r.Provider("west")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if c, err := p.DialContext(ctx, "tcp4", "10.55.0.1:7777"); err == nil {
		c.Close()
		t.Fatal("failed peer fell back")
	}
	p, _ = r.Provider("east")
	exchange(t, p, "tcp4", "east")
}
