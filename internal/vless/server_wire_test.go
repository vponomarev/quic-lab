package vless

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/mux"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/uuid"
	account "github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/proxy/vless/encoding"
	"io"
	"net"
	"testing"
	"time"
)

func udpEcho(t *testing.T) string {
	t.Helper()
	p, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Close() })
	go func() {
		b := make([]byte, 65535)
		for {
			n, a, e := p.ReadFrom(b)
			if e != nil {
				return
			}
			p.WriteTo(b[:n], a)
		}
	}()
	return p.LocalAddr().String()
}
func rawXUDP(t *testing.T, addr string, cert []byte, id string) (net.Conn, *protocol.RequestHeader) {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(cert)
	c, e := tls.Dial("tcp4", addr, &tls.Config{RootCAs: roots, ServerName: "fixture.invalid", MinVersion: tls.VersionTLS13})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(2 * time.Second))
	u, e := uuid.ParseString(id)
	if e != nil {
		t.Fatal(e)
	}
	h := &protocol.RequestHeader{Version: 0, Command: protocol.RequestCommandMux, User: &protocol.MemoryUser{Account: &account.MemoryAccount{ID: protocol.NewID(u)}}}
	if e := encoding.EncodeRequestHeader(c, h, &encoding.Addons{}); e != nil {
		t.Fatal(e)
	}
	return c, h
}
func TestServerXUDPSameGlobalIDDifferentDevices(t *testing.T) {
	s, addr, cert := serverFixture(t, "standalone", "")
	target, e := xnet.ParseDestination("udp:" + udpEcho(t))
	if e != nil {
		t.Fatal(e)
	}
	global := [8]byte{9, 8, 7, 6, 5, 4, 3, 2}
	a, ah := rawXUDP(t, addr, cert, serverID)
	b, bh := rawXUDP(t, addr, cert, otherServerID)
	read := func(c net.Conn, want string) {
		t.Helper()
		var m mux.FrameMetadata
		if e := m.Unmarshal(c, false); e != nil {
			t.Fatal(e)
		}
		mb, e := mux.NewPacketReader(&buf.BufferedReader{Reader: buf.NewReader(c)}, &target).ReadMultiBuffer()
		defer buf.ReleaseMulti(mb)
		if e != nil || func() string { p := make([]byte, mb.Len()); mb.Copy(p); return string(p) }() != want {
			t.Fatalf("XUDP reply: %v", e)
		}
	}
	for i, c := range []net.Conn{a, b} {
		c.Write(muxFrame(t, target, global, mux.SessionStatusNew, []byte("first")))
		h := ah
		if i == 1 {
			h = bh
		}
		if _, e := encoding.DecodeResponseHeader(c, h); e != nil {
			t.Fatal(e)
		}
		read(c, "first")
	}
	// Reusing another user's global ID must not detach the first user's stream.
	a.Write(muxFrame(t, target, [8]byte{}, mux.SessionStatusKeep, []byte("still-a")))
	read(a, "still-a")
	s.Revoke(serverID)
	a.SetReadDeadline(time.Now().Add(time.Second))
	if _, e := a.Read(make([]byte, 1)); e == nil {
		t.Fatal("revoked XUDP alive")
	} else if n, ok := e.(net.Error); ok && n.Timeout() {
		t.Fatal("XUDP revoke timed out")
	}
	b.Write(muxFrame(t, target, [8]byte{}, mux.SessionStatusKeep, []byte("still-b")))
	read(b, "still-b")
}
func TestServerOrdinaryUDPRevoke(t *testing.T) {
	t.Setenv("xray.cone.disabled", "true")
	TestServerUDPRevoke(t)
}
func TestServerWrongIdentity(t *testing.T) {
	_, addr, cert := serverFixture(t, "standalone", "")
	c, e := serverClient(t, addr, cert, "33333333-3333-4333-8333-333333333333").DialContext(context.Background(), "tcp4", tcpEcho(t))
	if e != nil {
		return
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	c.Write([]byte("no"))
	if _, e := c.Read(make([]byte, 2)); e == nil {
		t.Fatal("wrong UUID accepted")
	}
}
func TestServerRealityVision(t *testing.T) {
	cert, key := fixtureCertificate(t)
	pair, e := tls.X509KeyPair(cert, key)
	if e != nil {
		t.Fatal(e)
	}
	camouflage, e := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13, NextProtos: []string{"h2", "http/1.1"}})
	if e != nil {
		t.Fatal(e)
	}
	defer camouflage.Close()
	go func() {
		for {
			c, e := camouflage.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); c.SetDeadline(time.Now().Add(3 * time.Second)); io.Copy(io.Discard, c) }()
		}
	}()
	k, e := ecdh.X25519().GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	port, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := port.Addr().String()
	port.Close()
	s, e := StartServer(context.Background(), ServerOptions{Listen: addr, Mode: "standalone", Security: "reality", RealityTarget: camouflage.Addr().String(), RealityServerNames: []string{"fixture.invalid"}, RealityPrivateKey: k.Bytes(), RealityShortIDs: []string{"aabb"}, Clients: []ServerClient{{UUID: serverID, Flow: "xtls-rprx-vision"}}})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, e := New(context.Background(), Config{Endpoint: addr, UUID: serverID, Security: "reality", ServerName: "fixture.invalid", Fingerprint: "chrome", Flow: "xtls-rprx-vision", RealityPublicKey: base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), ShortID: "aabb"}, &net.Dialer{})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	for _, network := range []string{"tcp4", "udp4"} {
		target := tcpEcho(t)
		if network == "udp4" {
			target = udpEcho(t)
		}
		c, e := a.DialContext(context.Background(), network, target)
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		exchange(t, c)
	}
	s.Revoke(serverID)
	// The key and private connection metadata must never be logged by this test.

}
