package serverexit

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/adapter"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"quiclab/internal/icmprelay"
)

// ForwardPackets owns a per-device stack. Source /32 is checked a second time
// after worker-side AWG authentication. All IP traffic stays in the provider;
// neither kernel routes nor the system resolver participate in named egress.
func ForwardPackets(parent context.Context, lease Lease, wire net.Conn) error {
	if !lease.Source.Is4() || lease.Provider == nil {
		return ErrDenied
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { wire.Close() })
	defer stop()
	link := channel.New(512, 1280, "")
	h := &packetHandler{ctx: ctx, provider: lease.Provider, slots: make(chan struct{}, 128)}
	s, err := core.CreateStack(&core.Config{LinkEndpoint: link, TransportHandler: h})
	if err != nil {
		link.Close()
		return err
	}
	var writeMu sync.Mutex
	write := func(p []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		wire.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, err := wire.Write(p)
		return err
	}
	icmp := icmprelay.NewPacket(func(ctx context.Context, p []byte) ([]byte, error) {
		if dst, message, ok := icmprelay.Parse(p); ok && lease.Gateway.Is4() && dst == lease.Gateway {
			response := append([]byte(nil), message...)
			response[0], response[2], response[3] = 0, 0, 0
			binary.BigEndian.PutUint16(response[2:4], icmprelay.Checksum(response))
			return icmprelay.Reply(p, response), nil
		}
		return lease.Provider.ProbeICMP(ctx, p)
	}, write)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			packet := link.ReadContext(ctx)
			if packet == nil {
				return
			}
			view := packet.ToView()
			err := write(view.AsSlice())
			view.Release()
			packet.DecRef()
			if err != nil {
				cancel()
				return
			}
		}
	}()
	defer func() {
		h.mu.Lock()
		h.closed = true
		h.mu.Unlock()
		cancel()
		wire.Close()
		icmp.Close()
		s.Close()
		link.Close()
		s.Wait()
		h.wg.Wait()
		<-done
	}()
	buf := make([]byte, 65535)
	for {
		n, err := wire.Read(buf)
		if err != nil {
			return err
		}
		p := buf[:n]
		if len(p) < 20 || p[0]>>4 != 4 || int(binary.BigEndian.Uint16(p[2:4])) != len(p) || netip.AddrFrom4([4]byte(p[12:16])) != lease.Source {
			continue
		}
		if icmp.Handle(p) {
			continue
		}
		if p[9] != 6 && p[9] != 17 {
			continue
		}
		packet := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(append([]byte(nil), p...))})
		link.InjectInbound(ipv4.ProtocolNumber, packet)
		packet.DecRef()
	}
}

type packetHandler struct {
	ctx      context.Context
	provider Provider
	slots    chan struct{}
	mu       sync.Mutex
	closed   bool
	wg       sync.WaitGroup
}

func (h *packetHandler) start(network, address string, c net.Conn) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		c.Close()
		return
	}
	select {
	case h.slots <- struct{}{}:
	default:
		h.mu.Unlock()
		c.Close()
		return
	}
	h.wg.Add(1)
	h.mu.Unlock()
	go func() {
		defer h.wg.Done()
		defer func() { <-h.slots }()
		defer c.Close()
		ctx, cancel := context.WithTimeout(h.ctx, 8*time.Second)
		out, err := h.provider.DialContext(ctx, network, address)
		cancel()
		if err != nil {
			if out != nil {
				out.Close()
			}
			return
		}
		defer out.Close()
		relay(h.ctx, c, out, network == "udp4")
	}()
}
func (h *packetHandler) HandleTCP(c adapter.TCPConn) {
	id := c.ID()
	h.start("tcp4", net.JoinHostPort(id.LocalAddress.String(), strconv.Itoa(int(id.LocalPort))), c)
}
func (h *packetHandler) HandleUDP(c adapter.UDPConn) {
	id := c.ID()
	peer := &net.UDPAddr{IP: net.ParseIP(id.RemoteAddress.String()), Port: int(id.RemotePort)}
	h.start("udp4", net.JoinHostPort(id.LocalAddress.String(), strconv.Itoa(int(id.LocalPort))), &connectedPacket{PacketConn: c, peer: peer})
}

type connectedPacket struct {
	net.PacketConn
	peer net.Addr
}

func (c *connectedPacket) RemoteAddr() net.Addr        { return c.peer }
func (c *connectedPacket) Read(b []byte) (int, error)  { n, _, err := c.ReadFrom(b); return n, err }
func (c *connectedPacket) Write(b []byte) (int, error) { return c.WriteTo(b, c.peer) }
