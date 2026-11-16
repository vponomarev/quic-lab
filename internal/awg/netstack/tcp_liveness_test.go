package netstack

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
)

// Diagnostic observation, not a policy assertion: report the options actually
// applied by the AWG outbound dialer. Future liveness changes may alter them.
func TestOutboundTCPLivenessOptionsDiagnostic(t *testing.T) {
	ip := netip.MustParseAddr("10.55.0.2")
	dev, n, err := CreateNetTUN([]netip.Addr{ip}, nil, 1280)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	ln, err := n.ListenTCPAddrPort(netip.AddrPortFrom(ip, 8080))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := n.DialContextTCPAddrPort(ctx, netip.AddrPortFrom(ip, 8080))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	peer, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	local := c.LocalAddr().(*net.TCPAddr)
	addr := tcpip.AddrFrom4(ip.As4())
	endpoint := n.stack.FindTransportEndpoint(ipv4.ProtocolNumber, tcp.ProtocolNumber, stack.TransportEndpointID{LocalAddress: addr, LocalPort: uint16(local.Port), RemoteAddress: addr, RemotePort: 8080}, 1)
	ep, ok := endpoint.(tcpip.Endpoint)
	if !ok {
		t.Fatalf("unexpected endpoint %T", endpoint)
	}
	var idle tcpip.KeepaliveIdleOption
	var interval tcpip.KeepaliveIntervalOption
	if e := ep.GetSockOpt(&idle); e != nil {
		t.Fatal(e)
	}
	if e := ep.GetSockOpt(&interval); e != nil {
		t.Fatal(e)
	}
	count, e := ep.GetSockOptInt(tcpip.KeepaliveCountOption)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("AWG outbound TCP: keepalive_enabled=%t idle=%s interval=%s count=%d", ep.SocketOptions().GetKeepAlive(), time.Duration(idle), time.Duration(interval), count)
}
