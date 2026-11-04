//go:build linux

package serverexit

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"os"
	"quiclab/internal/awg"
	"quiclab/internal/awg/netstack"
	"quiclab/internal/awgserver"
	"quiclab/internal/icmprelay"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
)

// Both encrypted legs, the native worker router, framed broker and per-device
// forwarding stack run for real; no kernel routes or production peers are used.
func TestNativeAWGIngressThroughTwoRealExits(t *testing.T) {
	west, _ := realExit(t, "west", [4]byte{198, 51, 100, 10})
	east, _ := realExit(t, "east", [4]byte{198, 51, 100, 11})
	catalog, err := NewCatalog([]Definition{west, east})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	runtime, err := StartOwned(context.Background(), catalog, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	var mu sync.Mutex
	selected := "west"
	var oldClose func()
	broker := Broker{Packets: ForwardPackets, Acquire: func(protocol, id string, close func()) (Lease, error) {
		mu.Lock()
		defer mu.Unlock()
		if protocol != "awg" || id != "device" {
			return Lease{}, ErrDenied
		}
		p, err := runtime.Provider(selected)
		if err != nil {
			return Lease{}, err
		}
		oldClose = close
		return Lease{Provider: p, Source: netip.MustParseAddr("10.77.0.2"), Gateway: netip.MustParseAddr("10.77.0.1"), Release: func() {}}, nil
	}}
	stop, err := broker.Serve(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	kernel, _, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.77.0.1")}, nil, 1280)
	if err != nil {
		t.Fatal(err)
	}
	router := awgserver.NewExitTUN(kernel, func(ctx context.Context, id string) (net.Conn, error) {
		return (Client{Dir: dir}).Dial(ctx, "awg", id, "packet", "")
	})
	native := device.NewDevice(router, conn.NewStdNetBind(), &device.Logger{Verbosef: func(string, ...any) {}, Errorf: func(string, ...any) {}})
	defer native.Close()
	identity, _ := awgserver.NewIdentity()
	peer, _ := awgserver.NewPeer("10.77.0.2")
	cfg := awgserver.Config{Endpoint: "127.0.0.1:0", Address: "10.77.0.1/24", MTU: 1280, DNS: "10.55.0.1", AllowedIPs: []string{"0.0.0.0/0"}}
	state := awgserver.State{Version: 2, AWG: identity, Users: map[string]awgserver.User{"user": {ID: "user", Protocols: []string{"awg"}, Expires: time.Now().Add(time.Hour), ServerExitPolicy: []byte(`{"allowed":["west","east"],"default":"west"}`)}}, Devices: map[string]awgserver.Device{"device": {ID: "device", UserID: "user", AWG: peer, Expires: time.Now().Add(time.Hour)}}}
	worker := awgserver.Worker{Device: native, Exits: router, Config: cfg}
	if err = worker.Apply(state); err != nil {
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
	cfg.Endpoint = net.JoinHostPort("127.0.0.1", port)
	parsed, err := awg.Parse(cfg.Client(*identity, *peer))
	if err != nil {
		t.Fatal(err)
	}
	client, err := awg.Start(parsed, cfg.Endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	exchange := func(kind, want string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		c, err := client.DialContext(ctx, kind, "10.55.0.1:7777")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(4 * time.Second))
		c.Write([]byte("x"))
		b := make([]byte, len(want))
		if _, err = io.ReadFull(c, b); err != nil || string(b) != want {
			t.Fatal(kind, string(b), err)
		}
	}
	exchange("tcp4", "west")
	exchange("udp4", "west")
	packet := make([]byte, 44)
	packet[0], packet[8], packet[9], packet[20] = 0x45, 64, 1, 8
	binary.BigEndian.PutUint16(packet[2:4], 44)
	copy(packet[12:16], []byte{10, 77, 0, 2})
	copy(packet[16:20], []byte{10, 55, 0, 1})
	copy(packet[24:], []byte("named-exit-icmp-test"))
	binary.BigEndian.PutUint16(packet[10:12], icmprelay.Checksum(packet[:20]))
	binary.BigEndian.PutUint16(packet[22:24], icmprelay.Checksum(packet[20:]))
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if _, err = client.EchoPacket(ctx, packet); err != nil {
		t.Fatal("ICMP across both encrypted legs", err)
	}
	copy(packet[16:20], []byte{10, 77, 0, 1})
	packet[10], packet[11] = 0, 0
	binary.BigEndian.PutUint16(packet[10:12], icmprelay.Checksum(packet[:20]))
	if _, err = client.EchoPacket(ctx, packet); err != nil {
		t.Fatal("local gateway echo", err)
	}
	mu.Lock()
	selected = "east"
	closeOld := oldClose
	mu.Unlock()
	if closeOld != nil {
		closeOld()
	}
	d := state.Devices["device"]
	d.ServerExitID = "east"
	d.ServerExitRevision++
	state.Devices["device"] = d
	if err = worker.Apply(state); err != nil {
		t.Fatal(err)
	}
	exchange("tcp4", "east")
	exchange("udp4", "east")
	stop()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel2()
	if c, err := client.DialContext(ctx2, "tcp4", "10.77.0.1:9999"); err == nil {
		c.Close()
		t.Fatal("broker outage reached kernel stack")
	}
}
