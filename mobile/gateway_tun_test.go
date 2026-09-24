//go:build linux

package mobile

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/adapter"
	"github.com/xjasonlyu/tun2socks/v2/core/device/fdbased"
	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"quiclab/internal/gateway"
)

func TestTUNDNSRoundTripAndStop(t *testing.T) {
	var target net.IP
	addresses, _ := net.InterfaceAddrs()
	for _, a := range addresses {
		ip, _, _ := net.ParseCIDR(a.String())
		if ip.To4() != nil && !ip.IsLoopback() {
			target = ip.To4()
			break
		}
	}
	if target == nil {
		t.Skip("non-loopback IPv4 required for TUN destination")
	}
	dns, e := net.ListenPacket("udp4", net.JoinHostPort(target.String(), "53"))
	if e != nil {
		t.Skipf("isolated loopback DNS unavailable: %v", e)
	}
	defer dns.Close()
	go func() {
		b := make([]byte, 4096)
		n, a, e := dns.ReadFrom(b)
		if e == nil {
			dns.WriteTo(b[:n], a)
		}
	}()
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(ca, []byte(cp), 0600)
	tc, _ := gateway.TLS(pair, ca)
	srv, _ := gateway.New(target.String()+"/32", slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, e := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{MaxIncomingStreams: 128})
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	go srv.ServeQUIC(ctx, ln)
	g := NewGateway(nil)
	cfg, _ := json.Marshal(gatewayConfig{"quic", ln.Addr().String(), "localhost", cp, kp, cp})
	if e = g.Start(string(cfg), nil); e != nil {
		t.Fatal(e)
	}
	defer g.Stop()
	fds, e := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer unix.Close(fds[0])
	defer unix.Close(fds[1])
	if e = g.Attach(fds[0]); e != nil {
		t.Fatal(e)
	}
	payload := []byte("DNS roundtrip through TUN")
	p := make([]byte, 28+len(payload))
	p[0] = 0x45
	p[8] = 64
	p[9] = 17
	binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
	copy(p[12:16], []byte{10, 254, 254, 1})
	copy(p[16:20], target)
	var sum uint32
	for i := 0; i < 20; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(p[i : i+2]))
	}
	for sum > 65535 {
		sum = (sum & 65535) + (sum >> 16)
	}
	binary.BigEndian.PutUint16(p[10:12], ^uint16(sum))
	binary.BigEndian.PutUint16(p[20:22], 12345)
	binary.BigEndian.PutUint16(p[22:24], 53)
	binary.BigEndian.PutUint16(p[24:26], uint16(len(p)-20))
	copy(p[28:], payload)
	if _, e = unix.Write(fds[1], p); e != nil {
		t.Fatal(e)
	}
	poll := []unix.PollFd{{Fd: int32(fds[1]), Events: unix.POLLIN}}
	if n, e := unix.Poll(poll, 5000); e != nil || n == 0 {
		t.Fatalf("TUN reply timeout: %v", e)
	}
	b := make([]byte, 1500)
	n, e := unix.Read(fds[1], b)
	if e != nil {
		t.Fatal(e)
	}
	if n < 28 || string(b[28:n]) != string(payload) {
		t.Fatalf("invalid packet: %x", b[:n])
	}
	// Feed real TCP packets through the same TUN using a second userspace stack.
	tcpServer, e := net.Listen("tcp4", net.JoinHostPort(target.String(), "0"))
	if e != nil {
		t.Fatal(e)
	}
	defer tcpServer.Close()
	body := bytes.Repeat([]byte("TUN TCP response"), 10000)
	go func() {
		c, e := tcpServer.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		io.ReadAll(c)
		c.Write(body)
	}()
	clientFD, e := unix.Dup(fds[1])
	if e != nil {
		t.Fatal(e)
	}
	device, e := fdbased.Open(strconv.Itoa(clientFD), 1280, 0)
	if e != nil {
		t.Fatal(e)
	}
	clientLink := &ownedLinkEndpoint{LinkEndpoint: device}
	cs, e := core.CreateStack(&core.Config{LinkEndpoint: clientLink, TransportHandler: rejectInbound{}})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { cs.Close(); clientLink.Close(); cs.Wait() }()
	var nic tcpip.NICID
	for id := range cs.NICInfo() {
		nic = id
		break
	}
	if e := cs.AddProtocolAddress(nic, tcpip.ProtocolAddress{Protocol: ipv4.ProtocolNumber, AddressWithPrefix: tcpip.AddrFrom4([4]byte{10, 254, 254, 1}).WithPrefix()}, stack.AddressProperties{}); e != nil {
		t.Fatal(e)
	}
	dialCtx, dcancel := context.WithTimeout(ctx, 5*time.Second)
	defer dcancel()
	conn, e := gonet.DialContextTCP(dialCtx, cs, tcpip.FullAddress{NIC: nic, Addr: tcpip.AddrFrom4Slice(target), Port: uint16(tcpServer.Addr().(*net.TCPAddr).Port)}, ipv4.ProtocolNumber)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	conn.Write([]byte("request"))
	conn.CloseWrite()
	result, e := io.ReadAll(conn)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(result, body) {
		t.Fatalf("TUN TCP response: %d/%d", len(result), len(body))
	}
	done := make(chan struct{})
	go func() { g.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("TUN stop leaked worker")
	}
}

type rejectInbound struct{}

func (rejectInbound) HandleTCP(c adapter.TCPConn) { c.Close() }
func (rejectInbound) HandleUDP(c adapter.UDPConn) { c.Close() }
