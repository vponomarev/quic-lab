//go:build linux && !android

package mobile

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"quiclab/internal/icmprelay"
)

func linuxEcho(ttl byte) []byte {
	p := make([]byte, 32)
	p[0] = 0x45
	p[8] = ttl
	p[9] = 1
	binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
	copy(p[12:16], []byte{192, 0, 2, 1})
	copy(p[16:20], []byte{1, 1, 1, 1})
	p[20] = 8
	p[25] = 3
	p[27] = 7
	copy(p[28:], "ping")
	binary.BigEndian.PutUint16(p[22:24], icmprelay.Checksum(p[20:]))
	binary.BigEndian.PutUint16(p[10:12], icmprelay.Checksum(p[:20]))
	return p
}

type icmpDispatcher struct{ packets atomic.Int32 }

// fdbased.Close closes the FD, but Attach(nil) is what wakes a blocked poll.
type detachICMPEndpoint struct {
	*channel.Endpoint
	detached atomic.Bool
}

func (e *detachICMPEndpoint) Attach(d stack.NetworkDispatcher) {
	if d == nil {
		e.detached.Store(true)
	}
	e.Endpoint.Attach(d)
}

func TestLinuxICMPCloseDetachesReader(t *testing.T) {
	inner := &detachICMPEndpoint{Endpoint: channel.New(8, 1280, "")}
	link := newLinuxICMPLink(inner, nil, nil)
	link.Attach(&icmpDispatcher{})
	link.Close()
	if !inner.detached.Load() {
		t.Fatal("reader stop notification was swallowed")
	}
	link.Attach(nil) // stack can also remove the closed NIC
}

func (d *icmpDispatcher) DeliverNetworkPacket(_ tcpip.NetworkProtocolNumber, _ *stack.PacketBuffer) {
	d.packets.Add(1)
}
func (d *icmpDispatcher) DeliverLinkPacket(_ tcpip.NetworkProtocolNumber, _ *stack.PacketBuffer) {}
func injectICMPLink(e *channel.Endpoint, p []byte) {
	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(p)})
	defer pkt.DecRef()
	e.InjectInbound(header.IPv4ProtocolNumber, pkt)
}

func TestLinuxICMPInterceptsEchoAndRouterErrors(t *testing.T) {
	for _, kind := range []byte{0, 3, 11} {
		t.Run(strconv.Itoa(int(kind)), func(t *testing.T) {
			inner := channel.New(8, 1280, "")
			d := &icmpDispatcher{}
			request := linuxEcho(1)
			var written atomic.Int32
			link := newLinuxICMPLink(inner, func(ctx context.Context, p []byte) ([]byte, error) {
				if !bytes.Equal(p, request) {
					t.Error("request/TTL altered")
				}
				source := netip.MustParseAddr("1.1.1.1")
				msg := append([]byte(nil), p[20:]...)
				if kind != 0 {
					source = netip.MustParseAddr("10.8.0.1")
					msg = append(make([]byte, 8), p[:28]...)
				}
				msg[0] = kind
				msg[2] = 0
				msg[3] = 0
				binary.BigEndian.PutUint16(msg[2:4], icmprelay.Checksum(msg))
				return icmprelay.RestoreResponse(p, p[20:], source, 49, msg), nil
			}, func(n int) { written.Add(int32(n)) })
			defer link.Close()
			link.Attach(d)
			injectICMPLink(inner, request)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got := inner.ReadContext(ctx)
			if got == nil {
				t.Fatal("no tunnel reply")
			}
			defer got.DecRef()
			view := got.ToView()
			defer view.Release()
			reply := view.AsSlice()
			if !icmprelay.ValidateResponse(request, reply) || reply[8] != 49 || reply[20] != kind {
				t.Fatalf("invalid reply %x", reply)
			}
			if d.packets.Load() != 0 {
				t.Fatal("ICMP reached TCP/UDP stack")
			}
			link.Close()
			if written.Load() != int32(len(reply)-20) {
				t.Fatal("reply accounting")
			}
		})
	}
}
func TestLinuxICMPDropsInvalidAndNeverFabricates(t *testing.T) {
	inner := channel.New(8, 1280, "")
	d := &icmpDispatcher{}
	called := make(chan struct{}, 1)
	link := newLinuxICMPLink(inner, func(context.Context, []byte) ([]byte, error) {
		called <- struct{}{}
		return nil, errors.New("unavailable")
	}, nil)
	defer link.Close()
	link.Attach(d)
	bad := linuxEcho(64)
	bad[22] ^= 1
	injectICMPLink(inner, bad)
	injectICMPLink(inner, linuxEcho(64))
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("valid echo not forwarded")
	}
	udp := linuxEcho(64)
	udp[9] = 17
	injectICMPLink(inner, udp)
	if d.packets.Load() != 1 {
		t.Fatal("non-ICMP passthrough or malformed ICMP leak", d.packets.Load())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if p := inner.ReadContext(ctx); p != nil {
		p.DecRef()
		t.Fatal("fabricated reply")
	}
}

type icmpTestDialer func(context.Context, string, string) (net.Conn, error)

func (f icmpTestDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}

func TestLinuxICMPReplaceVPNCancelsAndSelectsCurrent(t *testing.T) {
	entered := make(chan struct{})
	old := NewGateway(nil)
	old.cfg.Transport = "quic"
	// Substitute the stream dial boundary; this never opens a network socket.
	old.direct = icmpTestDialer(func(ctx context.Context, _, _ string) (net.Conn, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	owner, binder := &desktopOwner{}, &multipleBinder{}
	r := desktopRouter(t, old, owner, binder)
	done := make(chan error, 1)
	go func() { _, err := r.echoLinux(context.Background(), linuxEcho(1)); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not reach old VPN")
	}
	replacement := NewGateway(nil)
	replacement.cfg.Transport = "https"
	want := errors.New("replacement selected")
	replacement.direct = icmpTestDialer(func(context.Context, string, string) (net.Conn, error) {
		return nil, want
	})
	replaced := make(chan error, 1)
	go func() { replaced <- r.ReplaceVPN(replacement) }()
	select {
	case err := <-replaced:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement did not cancel pending probe")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("old probe: %v", err)
	}
	if _, err := r.echoLinux(context.Background(), linuxEcho(1)); !errors.Is(err, want) {
		t.Fatalf("new probe: %v", err)
	}
	unsupported := NewGateway(nil)
	unsupported.cfg.Transport = "vless"
	if err := r.ReplaceVPN(unsupported); err != nil {
		t.Fatal(err)
	}
	if _, err := r.echoLinux(context.Background(), linuxEcho(64)); err == nil {
		t.Fatal("unsupported VPN accepted ICMP")
	}
	if err := r.ReplaceVPN(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.echoLinux(context.Background(), linuxEcho(64)); err == nil {
		t.Fatal("missing VPN accepted ICMP")
	}
	if owner.calls.Load() != 0 || binder.calls.Load() != 0 {
		t.Fatal("ICMP used application policy or direct fallback")
	}
}
func TestLinuxICMPCloseCancelsProbe(t *testing.T) {
	inner := channel.New(8, 1280, "")
	entered := make(chan struct{})
	done := make(chan struct{})
	link := newLinuxICMPLink(inner, func(ctx context.Context, _ []byte) ([]byte, error) {
		close(entered)
		<-ctx.Done()
		close(done)
		return nil, ctx.Err()
	}, nil)
	link.Attach(&icmpDispatcher{})
	injectICMPLink(inner, linuxEcho(64))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("probe not started")
	}
	closed := make(chan struct{})
	go func() { link.Close(); link.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close blocked")
	}
	select {
	case <-done:
	default:
		t.Fatal("probe not canceled")
	}
}
