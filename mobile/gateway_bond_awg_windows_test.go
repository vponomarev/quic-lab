//go:build windows

package mobile

import (
	"bufio"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"quiclab/internal/awg"
	"quiclab/internal/awg/netstack"
	"quiclab/internal/bond"
	"quiclab/internal/gateway"
	"quiclab/internal/protocol"
	"quiclab/internal/trafficbudget"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"golang.org/x/net/dns/dnsmessage"
)

// A Windows loopback fixture needs no interface restriction. Android's production
// Binder instead protects and binds the real physical network's sockets.
type awgLoopbackBinder struct{ meter *trafficbudget.Meter }

func (b awgLoopbackBinder) Bind(int64) error                { return nil }
func (b awgLoopbackBinder) InterfaceIndex() uint32          { return 0 }
func (b awgLoopbackBinder) CellMeter() *trafficbudget.Meter { return b.meter }

func startBondAWGPeer(t *testing.T, handler http.Handler, pair tls.Certificate) (string, string) {
	t.Helper()
	clientKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tun, network, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.55.0.1")}, nil, 1280)
	if err != nil {
		t.Fatal(err)
	}
	peer := device.NewDevice(tun, conn.NewDefaultBind(), &device.Logger{Verbosef: func(string, ...any) {}, Errorf: func(string, ...any) {}})
	t.Cleanup(peer.Close)
	ipc := fmt.Sprintf("private_key=%s\nlisten_port=0\nreplace_peers=true\npublic_key=%s\nallowed_ip=10.55.0.2/32\n", hex.EncodeToString(serverKey.Bytes()), hex.EncodeToString(clientKey.PublicKey().Bytes()))
	if err = peer.IpcSet(ipc); err != nil {
		t.Fatal("fixture IPC:", err)
	}
	if err = peer.Up(); err != nil {
		t.Fatal(err)
	}
	state, err := peer.IpcGet()
	if err != nil {
		t.Fatal(err)
	}
	port := ""
	for _, line := range strings.Split(state, "\n") {
		if strings.HasPrefix(line, "listen_port=") {
			port = strings.TrimPrefix(line, "listen_port=")
		}
	}
	if port == "" || port == "0" {
		t.Fatal("fixture UDP socket unavailable")
	}
	dns, err := network.ListenUDPAddrPort(netip.MustParseAddrPort("10.55.0.1:53"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dns.Close() })
	go func() {
		buffer := make([]byte, 2048)
		for {
			n, remote, err := dns.ReadFrom(buffer)
			if err != nil {
				return
			}
			var message dnsmessage.Message
			if message.Unpack(buffer[:n]) != nil || len(message.Questions) != 1 {
				continue
			}
			message.Header.Response = true
			message.Header.RecursionAvailable = true
			question := message.Questions[0]
			if question.Type == dnsmessage.TypeA {
				message.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{10, 55, 0, 1}}}}
			}
			response, err := message.Pack()
			if err == nil {
				dns.WriteTo(response, remote)
			}
		}
	}()
	listener, err := network.ListenTCPAddrPort(netip.MustParseAddrPort("10.55.0.1:443"))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	t.Cleanup(func() { server.Close() })
	roots := x509.NewCertPool()
	certificate, _ := x509.ParseCertificate(pair.Certificate[0])
	roots.AddCert(certificate)
	go server.Serve(tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.VerifyClientCertIfGiven, MinVersion: tls.VersionTLS13}))
	raw := fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = 10.55.0.2/32\nDNS = 10.55.0.1\n[Peer]\nPublicKey = %s\nEndpoint = private-peer.invalid:%s\nAllowedIPs = 10.55.0.1/32\n", base64.StdEncoding.EncodeToString(clientKey.Bytes()), base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes()), port)
	return raw, "127.0.0.1:" + port
}

// Missing AWG dispatch used to leave the reserve unable to carry an existing flow.
func TestBondAWGSharedExitContinuity(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	muxServer, _ := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	var capabilities atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/capabilities" {
			capabilities.Add(1)
			protocol.CapabilitiesHandler(protocol.Capabilities{ControlVersion: 1, DataVersion: 1}).ServeHTTP(w, r)
			return
		}
		muxServer.ServeBondHTTPS(w, r)
	})
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(cp))
	listener, err := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	defer server.Close()
	go server.Serve(listener)
	raw, outer := startBondAWGPeer(t, handler, pair)
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	var accepts atomic.Int32
	go func() {
		for {
			c, e := target.Accept()
			if e != nil {
				return
			}
			accepts.Add(1)
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	g := NewGateway(nil)
	defer g.Stop()
	cfg, _ := json.Marshal(gatewayConfig{Transport: "https", Endpoint: listener.Addr().String(), Hostname: "localhost", CA: cp, Certificate: cp, Key: kp, MaxAvailability: true})
	if err = g.Start(string(cfg), nil); err != nil {
		t.Fatal(err)
	}
	stream, err := g.dialStream(context.Background(), "tcp", target.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	exchange := func(payload string) {
		t.Helper()
		stream.SetDeadline(time.Now().Add(8 * time.Second))
		if _, e := stream.Write([]byte(payload)); e != nil {
			t.Fatal(e)
		}
		got := make([]byte, len(payload))
		if _, e := io.ReadFull(stream, got); e != nil || string(got) != payload {
			t.Fatal("stream payload:", e)
		}
	}
	exchange("before AWG")
	token, mux := g.bondToken, g.bond
	var envelope map[string]any
	json.Unmarshal(cfg, &envelope)
	envelope["endpoint"] = "private-mux.invalid:443"
	envelope["control_url"] = "https://localhost:443/api/v1/capabilities"
	envelope["underlay_awg_config"] = raw
	envelope["underlay_endpoint"] = outer
	cfg, _ = json.Marshal(envelope)
	json.Unmarshal(cfg, &g.cfg)
	meter := trafficbudget.NewMeter(trafficbudget.New("awg-carrier", 1<<20))
	info := bond.PathInfo{ID: "awg-reserve", ProfileID: "awg", Network: "cell", Generation: 1}
	if err = g.addBondNamedPath(context.Background(), info, awgLoopbackBinder{meter}, false); err != nil {
		t.Fatal(err)
	}
	if err = g.bond.Session.ProbeData(info.ID); err != nil {
		t.Fatal(err)
	}
	awaitRuntime(t, func() bool {
		for _, p := range g.bond.Session.Stats().Paths {
			if p.Name == info.ID && p.DataProbes > 0 {
				return true
			}
		}
		return false
	})
	g.DropBondPath("wifi")
	exchange(strings.Repeat("over AWG;", 8192))
	if accepts.Load() != 1 || g.bond != mux || g.bondToken != token {
		t.Fatalf("exit was replaced: accepts=%d", accepts.Load())
	}
	if meter.Ledger.Snapshot().Used == 0 {
		t.Fatal("encrypted outer UDP was not charged")
	}
	if capabilities.Load() != 1 {
		t.Fatal("private control endpoint was not fetched through AWG DNS", capabilities.Load())
	}
	g.bond.Session.RemoveNamedPath(info)
	// Closing a carrier must relinquish its exclusive identity before replacement.
	info.Generation++
	if err = g.addBondNamedPath(context.Background(), info, awgLoopbackBinder{meter}, false); err != nil {
		t.Fatal("replacement:", err)
	}
	exchange("after AWG replacement")
	meter.Receive(1<<20, trafficbudget.User)
	g.bond.Session.RemoveNamedPath(info)
	info.Generation++
	if err = g.addBondNamedPath(context.Background(), info, awgLoopbackBinder{meter}, false); err == nil {
		t.Fatal("exhausted AWG budget reopened")
	}
	t.Logf("destination_accepts=%d payload_exact=true session_unchanged=true encrypted_udp_metered=true", accepts.Load())
}

func TestBondAWGDialerExclusiveLifecycle(t *testing.T) {
	pair, cp, _ := testIdentity(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "independent peer alive") })
	raw, endpoint := startBondAWGPeer(t, handler, pair)
	_, closeFirst, err := newBondAWGDialer(raw, endpoint, awgLoopbackBinder{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeFirst()
	parsed, err := awg.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if standalone, e := awg.Start(parsed, endpoint, awgLoopbackBinder{}); e == nil {
		standalone.Close()
		t.Fatal("standalone engine bypassed shared peer lease")
	}
	if _, cleanup, err := newBondAWGDialer(raw+"\n# duplicate import\n", endpoint, awgLoopbackBinder{}); err == nil {
		cleanup()
		t.Fatal("same peer acquired two engines")
	}
	otherRaw, otherEndpoint := startBondAWGPeer(t, handler, pair)
	dialOther, closeOther, err := newBondAWGDialer(otherRaw, otherEndpoint, awgLoopbackBinder{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeOther()
	closeFirst()
	closeFirst()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := dialOther(ctx, "tcp4", "private-mux.invalid:443")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(cp))
	secure := tls.Client(connection, &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13})
	if _, err = io.WriteString(secure, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(secure), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "independent peer alive" {
		t.Fatal("closing one peer affected another", err)
	}
	_, replacement, err := newBondAWGDialer(raw, endpoint, awgLoopbackBinder{})
	if err != nil {
		t.Fatal("closed peer retained lease", err)
	}
	replacement()
}
