//go:build linux

package mobile

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"golang.org/x/sys/unix"
	"quiclab/internal/awg/netstack"
)

func TestAWGTUNDatagramBoundariesAndStop(t *testing.T) {
	clientKey, _ := ecdh.X25519().GenerateKey(rand.Reader)
	serverKey, _ := ecdh.X25519().GenerateKey(rand.Reader)
	tun, n, e := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.56.0.1")}, nil, 1280)
	if e != nil {
		t.Fatal(e)
	}
	d := device.NewDevice(tun, conn.NewStdNetBind(), &device.Logger{Verbosef: func(string, ...any) {}, Errorf: func(string, ...any) {}})
	defer d.Close()
	ipc := fmt.Sprintf("private_key=%s\npublic_key=%s\nallowed_ip=10.56.0.2/32\n", hex.EncodeToString(serverKey.Bytes()), hex.EncodeToString(clientKey.PublicKey().Bytes()))
	if e = d.IpcSet(ipc); e != nil {
		t.Fatal(e)
	}
	if e = d.Up(); e != nil {
		t.Fatal(e)
	}
	state, _ := d.IpcGet()
	port := ""
	for _, line := range strings.Split(state, "\n") {
		if strings.HasPrefix(line, "listen_port=") {
			port = strings.TrimPrefix(line, "listen_port=")
		}
	}
	raw := fmt.Sprintf("[Interface]\nPrivateKey=%s\nAddress=10.56.0.2\nDNS=10.56.0.1\n[Peer]\nPublicKey=%s\nEndpoint=127.0.0.1:%s\nAllowedIPs=10.56.0.1/32\n", base64.StdEncoding.EncodeToString(clientKey.Bytes()), base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes()), port)
	cfg, _ := json.Marshal(gatewayConfig{Transport: "awg", Endpoint: "127.0.0.1:" + port, AWGConfig: raw})
	g := NewGateway(nil)
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
	server, e := n.ListenUDPAddrPort(netip.MustParseAddrPort("10.56.0.1:9999"))
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	go func() {
		b := make([]byte, 256)
		_, a, e := server.ReadFrom(b)
		if e == nil {
			server.WriteTo([]byte("first"), a)
			server.WriteTo([]byte("second-longer"), a)
		}
	}()
	payload := []byte("request")
	p := make([]byte, 28+len(payload))
	p[0] = 0x45
	p[8] = 64
	p[9] = 17
	binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
	copy(p[12:16], []byte{10, 254, 254, 1})
	copy(p[16:20], []byte{10, 56, 0, 1})
	var sum uint32
	for i := 0; i < 20; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(p[i : i+2]))
	}
	for sum > 65535 {
		sum = (sum & 65535) + (sum >> 16)
	}
	binary.BigEndian.PutUint16(p[10:12], ^uint16(sum))
	binary.BigEndian.PutUint16(p[20:22], 12345)
	binary.BigEndian.PutUint16(p[22:24], 9999)
	binary.BigEndian.PutUint16(p[24:26], uint16(len(p)-20))
	copy(p[28:], payload)
	if _, e = unix.Write(fds[1], p); e != nil {
		t.Fatal(e)
	}
	for _, expected := range []string{"first", "second-longer"} {
		poll := []unix.PollFd{{Fd: int32(fds[1]), Events: unix.POLLIN}}
		if n, e := unix.Poll(poll, 5000); e != nil || n == 0 {
			t.Fatal("TUN UDP reply timeout", e)
		}
		b := make([]byte, 1500)
		n, e := unix.Read(fds[1], b)
		if e != nil || n < 28 || string(b[28:n]) != expected {
			t.Fatal("UDP boundary/unsolicited response lost", e)
		}
	}
	// TCP uses the same backend and preserves half-close across AWG.
	ln, e := n.ListenTCPAddrPort(netip.MustParseAddrPort("10.56.0.1:8080"))
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	body := bytes.Repeat([]byte("response"), 1000)
	go func() {
		c, e := ln.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		io.ReadAll(c)
		c.Write(body)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, e := g.dialStream(ctx, "tcp", "10.56.0.1:8080")
	if e != nil {
		t.Fatal(e)
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(5 * time.Second))
	stream.Write([]byte("request"))
	stream.CloseWrite()
	got, e := io.ReadAll(stream)
	if e != nil || !bytes.Equal(got, body) {
		t.Fatal("AWG TCP half-close", e)
	}
	done := make(chan struct{})
	go func() { g.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("AWG TUN shutdown blocked")
	}
}
