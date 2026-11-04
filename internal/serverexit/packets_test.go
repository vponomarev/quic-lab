package serverexit

import (
	"context"
	"io"
	"net"
	"net/netip"
	"quiclab/internal/awg/netstack"
	"testing"
	"time"
)

type packetTestProvider struct {
	fakeProvider
	tcp, udp string
}

func (p *packetTestProvider) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network == "tcp4" {
		address = p.tcp
	} else {
		address = p.udp
	}
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

func TestForwardPacketsTCPUDPAndSourceIsolation(t *testing.T) {
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	go func() {
		for {
			c, err := tcp.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	go func() {
		b := make([]byte, 4096)
		for {
			n, a, err := udp.ReadFrom(b)
			if err != nil {
				return
			}
			udp.WriteTo(b[:n], a)
		}
	}()
	for _, source := range []string{"10.22.0.2", "10.22.0.3"} {
		t.Run(source, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tun, network, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr(source)}, nil, 1280)
			if err != nil {
				t.Fatal(err)
			}
			defer tun.Close()
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			left, right := &packetConn{Conn: a}, &packetConn{Conn: b}
			done := make(chan struct{})
			go func() {
				defer close(done)
				ForwardPackets(ctx, Lease{Source: netip.MustParseAddr("10.22.0.2"), Provider: &packetTestProvider{tcp: tcp.Addr().String(), udp: udp.LocalAddr().String()}}, left)
			}()
			go func() {
				buf := make([]byte, 65535)
				for {
					n, err := right.Read(buf)
					if err != nil {
						return
					}
					if _, err = tun.Write([][]byte{buf[:n]}, 0); err != nil {
						return
					}
				}
			}()
			go func() {
				buf := make([]byte, 65535)
				sizes := []int{0}
				for {
					n, err := tun.Read([][]byte{buf}, sizes, 0)
					if err != nil {
						return
					}
					if n > 0 {
						if _, err = right.Write(buf[:sizes[0]]); err != nil {
							return
						}
					}
				}
			}()
			for _, kind := range []string{"tcp4", "udp4"} {
				dialCtx, stop := context.WithTimeout(ctx, 300*time.Millisecond)
				c, err := network.DialContext(dialCtx, kind, "198.18.0.1:7777")
				stop()
				if source != "10.22.0.2" && err != nil {
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				c.SetDeadline(time.Now().Add(300 * time.Millisecond))
				c.Write([]byte("payload"))
				buf := make([]byte, 7)
				_, err = io.ReadFull(c, buf)
				c.Close()
				if source == "10.22.0.2" && (err != nil || string(buf) != "payload") {
					t.Fatal(kind, err, string(buf))
				}
				if source != "10.22.0.2" && err == nil {
					t.Fatal("spoofed source forwarded")
				}
			}
			cancel()
			a.Close()
			b.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("packet stack did not stop")
			}
		})
	}
}
