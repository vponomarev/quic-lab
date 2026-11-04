//go:build linux && !android

package vless

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/xtls/xray-core/app/dispatcher"
	"github.com/xtls/xray-core/app/proxyman"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/proxy/freedom"
	account "github.com/xtls/xray-core/proxy/vless"
	inbound "github.com/xtls/xray-core/proxy/vless/inbound"
	"github.com/xtls/xray-core/transport/internet"
	xtls "github.com/xtls/xray-core/transport/internet/tls"
	"github.com/xtls/xray-core/transport/internet/websocket"
	"quiclab/internal/awg/netstack"
)

// Explicit opt-in, private namespaces and ephemeral credentials only.
func TestDesktopNamespaceFixture(t *testing.T) {
	dir := os.Getenv("VPN_PROBE_FIXTURE_DIR")
	if dir == "" {
		t.Skip("isolated live fixture only")
	}
	here, e := os.Readlink("/proc/self/ns/net")
	host, e2 := os.Readlink("/proc/1/ns/net")
	if e != nil || e2 != nil || here == host {
		t.Fatal("private network namespace required")
	}
	mode := os.Getenv("VPN_PROBE_TRANSPORT")
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer cancel()
	profile := map[string]any{"endpoint": "198.18.0.2:9443", "targets": []string{"192.0.2.2/32"}}
	var tcp net.Listener
	var udp net.PacketConn
	if mode == "awg" {
		clientKey, _ := ecdh.X25519().GenerateKey(rand.Reader)
		serverKey, _ := ecdh.X25519().GenerateKey(rand.Reader)
		tun, stack, e := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("192.0.2.2")}, nil, 1280)
		if e != nil {
			t.Fatal(e)
		}
		d := device.NewDevice(tun, conn.NewStdNetBind(), &device.Logger{Verbosef: func(string, ...any) {}, Errorf: func(string, ...any) {}})
		defer d.Close()
		if e = d.IpcSet(fmt.Sprintf("private_key=%s\nlisten_port=9443\npublic_key=%s\nallowed_ip=10.55.0.2/32\n", hex.EncodeToString(serverKey.Bytes()), hex.EncodeToString(clientKey.PublicKey().Bytes()))); e != nil {
			t.Fatal(e)
		}
		if e = d.Up(); e != nil {
			t.Fatal(e)
		}
		tcp, e = stack.ListenTCPAddrPort(netip.MustParseAddrPort("192.0.2.2:9001"))
		if e != nil {
			t.Fatal(e)
		}
		udp, e = stack.ListenUDPAddrPort(netip.MustParseAddrPort("192.0.2.2:9002"))
		if e != nil {
			tcp.Close()
			t.Fatal(e)
		}
		profile["transport"] = "awg"
		profile["awg_config"] = fmt.Sprintf("[Interface]\nPrivateKey=%s\nAddress=10.55.0.2/32\nDNS=192.0.2.2\n[Peer]\nPublicKey=%s\nEndpoint=198.18.0.2:9443\nAllowedIPs=192.0.2.2/32\n", base64.StdEncoding.EncodeToString(clientKey.Bytes()), base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes()))
	} else if mode == "vless" || mode == "vless-ws" {
		cert, key := fixtureCertificate(t)
		sec := serial.ToTypedMessage(&xtls.Config{Certificate: []*xtls.Certificate{{Certificate: cert, Key: key, OneTimeLoading: true}}})
		stream := &internet.StreamConfig{ProtocolName: "tcp", SecurityType: sec.Type, SecuritySettings: []*serial.TypedMessage{sec}}
		kind := "tcp"
		if mode == "vless-ws" {
			kind = "ws"
			stream.ProtocolName = "websocket"
			stream.TransportSettings = []*internet.TransportConfig{{ProtocolName: "websocket", Settings: serial.ToTypedMessage(&websocket.Config{Path: "/tunnel", Host: "cover.invalid"})}}
		}
		cfg := &core.Config{App: []*serial.TypedMessage{serial.ToTypedMessage(&dispatcher.Config{}), serial.ToTypedMessage(&proxyman.OutboundConfig{}), serial.ToTypedMessage(&proxyman.InboundConfig{})}, Inbound: []*core.InboundHandlerConfig{{
			ReceiverSettings: serial.ToTypedMessage(&proxyman.ReceiverConfig{Listen: xnet.NewIPOrDomain(xnet.ParseAddress("198.18.0.2")), PortList: &xnet.PortList{Range: []*xnet.PortRange{{From: 9443, To: 9443}}}, StreamSettings: stream}),
			ProxySettings:    serial.ToTypedMessage(&inbound.Config{Decryption: "none", Clients: []*protocol.User{{Account: serial.ToTypedMessage(&account.Account{Id: "11111111-1111-4111-8111-111111111111", Encryption: "none"})}}}),
		}}, Outbound: []*core.OutboundHandlerConfig{{ProxySettings: serial.ToTypedMessage(&freedom.Config{})}}}
		server, e := newEngine(ctx, cfg, &serverSockets{net.Dialer{Timeout: 5 * time.Second}})
		if e != nil {
			t.Fatal(e)
		}
		defer server.Close()
		tcp, e = net.Listen("tcp4", "192.0.2.2:9001")
		if e != nil {
			t.Fatal(e)
		}
		udp, e = net.ListenPacket("udp4", "192.0.2.2:9002")
		if e != nil {
			tcp.Close()
			t.Fatal(e)
		}
		profile["transport"] = "vless"
		profile["ca"] = string(cert)
		uri := "vless://11111111-1111-4111-8111-111111111111@198.18.0.2:9443?security=tls&sni=fixture.invalid&type=" + kind
		if kind == "ws" {
			uri += "&path=%2Ftunnel&host=cover.invalid"
		}
		profile["vless_uri"] = uri
	} else {
		t.Fatal("unsupported fixture transport")
	}
	defer tcp.Close()
	defer udp.Close()
	go func() {
		for {
			c, e := tcp.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	go func() {
		b := make([]byte, 4096)
		for {
			n, a, e := udp.ReadFrom(b)
			if e != nil {
				return
			}
			udp.WriteTo(b[:n], a)
		}
	}()
	raw, _ := json.Marshal(profile)
	tmp := filepath.Join(dir, "client.pending.json")
	if e := os.WriteFile(tmp, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Rename(tmp, filepath.Join(dir, "client.json")); e != nil {
		t.Fatal(e)
	}
	select {
	case <-ctx.Done():
	case <-time.After(90 * time.Second):
		t.Fatal("fixture timeout")
	}
}
