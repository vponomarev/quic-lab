//go:build linux && !android

package mobile

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"quiclab/internal/approuting"
)

type desktopOwner struct {
	calls atomic.Int32
	wait  bool
}

func (o *desktopOwner) Resolve(ctx context.Context, f approuting.FlowKey) approuting.Attribution {
	o.calls.Add(1)
	if o.wait {
		<-ctx.Done()
		return approuting.Attribution{Quality: approuting.Unknown, Reason: "canceled"}
	}
	path := `C:\app\other.exe`
	if f.Source.Port() == 1001 {
		path = `C:\app\selected.exe`
	}
	return approuting.Attribution{Quality: approuting.Resolved, Owners: []approuting.ProcessIdentity{{Path: path}}}
}

type desktopTCP struct {
	net.Conn
	id stack.TransportEndpointID
}

func (c desktopTCP) ID() *stack.TransportEndpointID { return &c.id }

type desktopUDP struct {
	*net.UDPConn
	id stack.TransportEndpointID
}

func (c desktopUDP) ID() *stack.TransportEndpointID { return &c.id }
func desktopID(src uint16, dst net.Addr) stack.TransportEndpointID {
	a := netip.MustParseAddrPort(dst.String())
	return stack.TransportEndpointID{RemoteAddress: tcpip.AddrFrom4([4]byte{10, 0, 0, 2}), RemotePort: src, LocalAddress: tcpip.AddrFrom4(a.Addr().As4()), LocalPort: a.Port()}
}
func desktopRouter(t *testing.T, g *Gateway, owner approuting.OwnerResolver, b *multipleBinder) *DesktopAppRouter {
	t.Helper()
	policy, e := approuting.CompileTUN(approuting.TUNPolicy{Version: 2, Enabled: true, Mode: approuting.OnlySelected, Only: []string{`C:\app\selected.exe`}})
	if e != nil {
		t.Fatal(e)
	}
	r, e := NewDesktopAppRouter(policy, owner, b, g, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(r.Close)
	return r
}

func TestDesktopAppRouterTCPUDPAndSeparateCounters(t *testing.T) {
	for _, transport := range []string{"quic", "https"} {
		t.Run(transport, func(t *testing.T) {
			g := multipleTestGateway(t, transport)
			owner := &desktopOwner{}
			binder := &multipleBinder{}
			r := desktopRouter(t, g, owner, binder)
			listener, e := net.Listen("tcp4", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer listener.Close()
			go func() {
				for {
					c, e := listener.Accept()
					if e != nil {
						return
					}
					go func() { defer c.Close(); io.Copy(c, c) }()
				}
			}()
			for _, src := range []uint16{1001, 1002} {
				client, in := net.Pipe()
				r.HandleTCP(desktopTCP{in, desktopID(src, listener.Addr())})
				client.SetDeadline(time.Now().Add(3 * time.Second))
				payload := []byte("TCP through selected exit")
				if _, e = client.Write(payload); e != nil {
					t.Fatal(e)
				}
				reply := make([]byte, len(payload))
				if _, e = io.ReadFull(client, reply); e != nil || !bytes.Equal(payload, reply) {
					t.Fatal(e)
				}
				client.Close()
			}
			udp, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if e != nil {
				t.Fatal(e)
			}
			defer udp.Close()
			response := bytes.Repeat([]byte("large UDP reply"), 2000)
			go func() {
				b := make([]byte, 65535)
				for {
					_, from, e := udp.ReadFrom(b)
					if e != nil {
						return
					}
					udp.WriteTo(response, from)
				}
			}()
			for _, src := range []uint16{1001, 1002} {
				in, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
				if e != nil {
					t.Fatal(e)
				}
				client, e := net.DialUDP("udp4", nil, in.LocalAddr().(*net.UDPAddr))
				if e != nil {
					t.Fatal(e)
				}
				r.HandleUDP(desktopUDP{in, desktopID(src, udp.LocalAddr())})
				client.SetDeadline(time.Now().Add(5 * time.Second))
				client.Write([]byte("request"))
				b := make([]byte, 65535)
				n, e := client.Read(b)
				if e != nil || !bytes.Equal(b[:n], response) {
					t.Fatalf("UDP: %d %v", n, e)
				}
				client.Close()
			}
			r.Close()
			s := r.Snapshot()
			if s.DirectTxBytes != 32 || s.DirectRxBytes != 30025 {
				t.Fatalf("direct counts %+v", s)
			}
			if r.vpn.h.tx.Load() != 32 || r.vpn.h.rx.Load() != 30025 || binder.calls.Load() != 2 || owner.calls.Load() != 4 {
				t.Fatal("counts leaked across exits", r.vpn.h.tx.Load(), r.vpn.h.rx.Load(), binder.calls.Load(), owner.calls.Load())
			}
		})
	}
}

func TestDesktopAppRouterDNSFailureAndSaturation(t *testing.T) {
	g := NewGateway(nil)
	g.cfg.Transport = "vless" // disconnected VPN, must never use direct
	owner := &desktopOwner{}
	binder := &multipleBinder{}
	r := desktopRouter(t, g, owner, binder)
	dns := approuting.FlowKey{Protocol: 17, Source: netip.MustParseAddrPort("10.0.0.2:1002"), Destination: netip.MustParseAddrPort("1.1.1.1:53")}
	if r.selectFlow(dns) != r.vpn.h || owner.calls.Load() != 0 {
		t.Fatal("DNS must remain global")
	}
	client, in := net.Pipe()
	defer client.Close()
	r.HandleTCP(desktopTCP{in, desktopID(1001, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9})})
	client.SetDeadline(time.Now().Add(time.Second))
	if _, e := client.Read(make([]byte, 1)); e == nil {
		t.Fatal("failed VPN still open")
	}
	if binder.calls.Load() != 0 {
		t.Fatal("VPN failed over to direct")
	}
	slow := &desktopOwner{wait: true}
	r2 := desktopRouter(t, g, slow, binder)
	peers := []net.Conn{}
	defer func() {
		for _, p := range peers {
			p.Close()
		}
	}()
	for i := 0; i < 100; i++ {
		a, b := net.Pipe()
		peers = append(peers, a)
		r2.HandleTCP(desktopTCP{b, desktopID(1001, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9})})
	}
	if r2.Snapshot().Overloaded == 0 {
		t.Fatal("pending queue not bounded")
	}
	done := make(chan struct{})
	go func() { r2.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown stuck in attribution")
	}
}
