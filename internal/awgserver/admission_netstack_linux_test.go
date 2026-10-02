//go:build linux

package awgserver

import (
	"context"
	"errors"
	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"net/netip"
	"quiclab/internal/awg"
	"quiclab/internal/awg/netstack"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Uses real encrypted AWG peers and in-process network stacks. No kernel TUN,
// namespace, routes, host addresses, or NET_ADMIN changes are required.
func TestAWGAuthenticatedPeerAdmissionNetstack(t *testing.T) {
	serverTUN, network, e := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.77.0.1")}, nil, 1280)
	if e != nil {
		t.Fatal(e)
	}
	var allow atomic.Bool
	attempts := make(chan string, 4)
	gate := NewAdmissionTUN(serverTUN, func(id string) (time.Duration, error) {
		select {
		case attempts <- id:
		default:
		}
		if !allow.Load() {
			return 0, errors.New("cap full")
		}
		return 2 * time.Second, nil
	}, 75*time.Second)
	logger := &device.Logger{Verbosef: func(string, ...any) {}, Errorf: func(string, ...any) {}}
	native := device.NewDevice(gate, conn.NewStdNetBind(), logger)
	defer native.Close()
	identity, _ := NewIdentity()
	peer, _ := NewPeer("10.77.0.2")
	cfg := Config{Endpoint: "127.0.0.1:0", Address: "10.77.0.1/24", MTU: 1280, DNS: "10.77.0.1", AllowedIPs: []string{"10.77.0.1/32"}}
	state := State{AWG: identity, Users: map[string]User{"phone": {ID: "phone", Protocols: []string{"awg"}, Expires: time.Now().Add(time.Hour), AWG: peer}}}
	worker := Worker{Device: native, Admission: gate, Config: cfg}
	if e = worker.Apply(state); e != nil {
		t.Fatal(e)
	}
	if e = native.Up(); e != nil {
		t.Fatal(e)
	}
	ipc, e := native.IpcGet()
	if e != nil {
		t.Fatal(e)
	}
	port := ""
	for _, line := range strings.Split(ipc, "\n") {
		if strings.HasPrefix(line, "listen_port=") {
			port = strings.TrimPrefix(line, "listen_port=")
		}
	}
	clientText := strings.Replace(cfg.Client(*identity, *peer), "Endpoint = 127.0.0.1:0", "Endpoint = 127.0.0.1:"+port, 1)
	clientCfg, e := awg.Parse(clientText)
	if e != nil {
		t.Fatal(e)
	}
	client, e := awg.Start(clientCfg, "127.0.0.1:"+port, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	listener, e := network.ListenUDPAddrPort(netip.MustParseAddrPort("10.77.0.1:7777"))
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	socket, e := client.DialContext(ctx, "udp4", "10.77.0.1:7777")
	if e != nil {
		t.Fatal(e)
	}
	defer socket.Close()
	socket.Write([]byte("denied"))
	select {
	case id := <-attempts:
		if id != "phone" {
			t.Fatal("wrong authenticated identity", id)
		}
	case <-ctx.Done():
		t.Fatal("AWG did not reach authenticated admission gate")
	}
	buffer := make([]byte, 32)
	listener.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, e = listener.ReadFrom(buffer); e == nil {
		t.Fatal("denied authenticated AWG packet reached server")
	}
	allow.Store(true)
	time.Sleep(time.Second)
	socket.Write([]byte("allowed"))
	listener.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, addr, e := listener.ReadFrom(buffer)
	if e != nil || string(buffer[:n]) != "allowed" {
		t.Fatal("admitted AWG packet missing", e)
	}
	listener.WriteTo([]byte("reply"), addr)
	socket.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, e = socket.Read(buffer)
	if e != nil || string(buffer[:n]) != "reply" {
		t.Fatal("admitted outbound AWG packet missing", e)
	}
	// Source-IP spoofing remains rejected in the native peer receive path before
	// the wrapper sees plaintext, even though the handshake keys remain valid.
	badCfg := *clientCfg
	badCfg.Address = "10.77.0.9"
	bad, e := awg.Start(&badCfg, "127.0.0.1:"+port, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer bad.Close()
	badSocket, e := bad.DialContext(ctx, "udp4", "10.77.0.1:7777")
	if e != nil {
		t.Fatal(e)
	}
	defer badSocket.Close()
	badSocket.Write([]byte("spoof"))
	listener.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, e = listener.ReadFrom(buffer); e == nil {
		t.Fatal("spoofed authenticated source reached server")
	}
}
