//go:build linux

package awgserver

import (
	"context"
	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/tun"
	"io"
	"net"
	"os"
	"os/exec"
	"quiclab/internal/awg"
	"testing"
	"time"
)

// Run only inside an isolated network namespace/container with NET_ADMIN and /dev/net/tun.
func TestLinuxWorkerLifecycle(t *testing.T) {
	if os.Getenv("QUIC_LAB_AWG_NET_TEST") != "1" {
		t.Skip("isolated Linux network test")
	}
	c := Config{Endpoint: "127.0.0.1:51820", Address: "10.77.0.1/24", DNS: "1.1.1.1", AllowedIPs: []string{"0.0.0.0/0"}, Interface: "ql-awg0", MTU: 1280}
	iface, e := tun.CreateTUN(c.Interface, c.MTU)
	if e != nil {
		t.Fatal(e)
	}
	d := device.NewDevice(iface, conn.NewStdNetBind(), &device.Logger{Verbosef: func(string, ...any) {}, Errorf: func(string, ...any) {}})
	defer d.Close()
	identity, _ := NewIdentity()
	peer, _ := NewPeer("10.77.0.2")
	state := State{AWG: identity, Users: map[string]User{"one": {ID: "one", Protocols: []string{"awg"}, Expires: time.Now().Add(time.Hour), AWG: peer}}}
	w := Worker{Device: d, Config: c}
	if e = w.Apply(state); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"-4", "address", "add", c.Address, "dev", c.Interface}, {"link", "set", "dev", c.Interface, "up"}} {
		if e = exec.Command("ip", args...).Run(); e != nil {
			t.Fatal(e)
		}
	}
	if e = d.Up(); e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp4", "10.77.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	go func() {
		for {
			socket, e := listener.Accept()
			if e != nil {
				return
			}
			go func() { defer socket.Close(); io.Copy(socket, socket) }()
		}
	}()
	cfg, e := awg.Parse(c.Client(*identity, *peer))
	if e != nil {
		t.Fatal(e)
	}
	engine, e := awg.Start(cfg, c.Endpoint, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer engine.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	socket, e := engine.DialContext(ctx, "tcp", listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer socket.Close()
	exchange := func() {
		t.Helper()
		socket.SetDeadline(time.Now().Add(5 * time.Second))
		if _, e := socket.Write([]byte("continuity")); e != nil {
			t.Fatal(e)
		}
		b := make([]byte, 10)
		if _, e := io.ReadFull(socket, b); e != nil || string(b) != "continuity" {
			t.Fatal("echo", e)
		}
	}
	exchange()
	extra, _ := NewPeer("10.77.0.3")
	state.Users["two"] = User{ID: "two", Protocols: []string{"awg"}, Expires: time.Now().Add(time.Hour), AWG: extra}
	if e = w.Apply(state); e != nil {
		t.Fatal(e)
	}
	exchange()
	if e = engine.Migrate(nil); e != nil {
		t.Fatal(e)
	}
	exchange()
	status, e := w.Snapshot(state)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, p := range status.Peers {
		if p.ID == "one" {
			found = p.TX > 0 && p.RX > 0 && p.Source != "" && !p.Handshake.IsZero()
		}
	}
	if !found {
		t.Fatal("missing handshake/traffic/source statistics")
	}
	u := state.Users["one"]
	u.Disabled = true
	state.Users["one"] = u
	if e = w.Apply(state); e != nil {
		t.Fatal(e)
	}
	socket.SetDeadline(time.Now().Add(500 * time.Millisecond))
	socket.Write([]byte("revoked"))
	b := make([]byte, 7)
	if _, e = socket.Read(b); e == nil {
		t.Fatal("revoked peer still receives data")
	}
	delete(state.Users, "one")
	if e = w.Apply(state); e != nil {
		t.Fatal(e)
	}
	if len(w.peers) != 1 {
		t.Fatal("wrong peer removed")
	}
}
