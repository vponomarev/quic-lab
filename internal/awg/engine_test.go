package awg

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestEnginePeerTrafficRebindAndStop(t *testing.T) {
	a, _ := ecdh.X25519().GenerateKey(rand.Reader)
	b, _ := ecdh.X25519().GenerateKey(rand.Reader)
	cfg := func(address string, private, public []byte, allowed string) *Config {
		return &Config{Address: address, DNS: "10.55.0.1", MTU: 1280, private: hex.EncodeToString(private), public: hex.EncodeToString(public), Allowed: []string{allowed}, params: map[string]string{"jc": "2", "jmin": "50", "jmax": "100", "s1": "134", "s2": "90", "h1": "101", "h2": "102", "h3": "103", "h4": "104"}}
	}
	server, e := Start(cfg("10.55.0.1", b.Bytes(), a.PublicKey().Bytes(), "10.55.0.2/32"), "127.0.0.1:9", nil)
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	ipc, e := server.device.IpcGet()
	if e != nil {
		t.Fatal(e)
	}
	port := ""
	for _, line := range strings.Split(ipc, "\n") {
		if strings.HasPrefix(line, "listen_port=") {
			port = strings.TrimPrefix(line, "listen_port=")
		}
	}
	client, e := Start(cfg("10.55.0.2", a.Bytes(), b.PublicKey().Bytes(), "10.55.0.1/32"), "127.0.0.1:"+port, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	ln, e := server.network.ListenTCPAddrPort(netip.MustParseAddrPort("10.55.0.1:8080"))
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	go func() {
		c, e := ln.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		io.Copy(c, c)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	c, e := client.DialContext(ctx, "tcp4", "10.55.0.1:8080")
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	for i := 0; i < 3; i++ {
		if _, e = c.Write([]byte("test")); e != nil {
			t.Fatal(e)
		}
		buf := make([]byte, 4)
		if _, e = io.ReadFull(c, buf); e != nil || string(buf) != "test" {
			t.Fatal("TCP roundtrip", e)
		}
		if e = client.Migrate(nil); e != nil {
			t.Fatal(e)
		}
	}
	udp, e := server.network.ListenUDPAddrPort(netip.MustParseAddrPort("10.55.0.1:9999"))
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	go func() {
		buf := make([]byte, 256)
		n, a, e := udp.ReadFrom(buf)
		if e == nil {
			udp.WriteTo(buf[:n], a)
			udp.WriteTo([]byte("unsolicited"), a)
		}
	}()
	u, e := client.DialContext(ctx, "udp4", "10.55.0.1:9999")
	if e != nil {
		t.Fatal(e)
	}
	defer u.Close()
	u.SetDeadline(time.Now().Add(3 * time.Second))
	u.Write([]byte("udp"))
	for _, expected := range []string{"udp", "unsolicited"} {
		buf := make([]byte, 256)
		n, e := u.Read(buf)
		if e != nil || string(buf[:n]) != expected {
			t.Fatal("UDP datagram", e)
		}
	}
	if _, e = client.DialContext(ctx, "udp4", "192.0.2.1:53"); e == nil {
		t.Fatal("AllowedIPs bypass")
	}
	done := make(chan struct{})
	go func() { client.Close(); client.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown blocked")
	}
}
