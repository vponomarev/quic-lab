package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"quiclab/internal/echo"
	"quiclab/internal/gateway"
	"quiclab/internal/labcert"
	"quiclab/internal/protocol"
	"quiclab/internal/servertls"
)

func sharedQUICCertificates(t *testing.T) (*tls.Config, *tls.Config, tls.Certificate) {
	t.Helper()
	cp, kp, err := labcert.Generate()
	if err != nil {
		t.Fatal(err)
	}
	server, err := tls.X509KeyPair(cp, kp)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test client CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "test device"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &clientKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return &tls.Config{Certificates: []tls.Certificate{server}, NextProtos: []string{protocol.ALPN}, MinVersion: tls.VersionTLS13},
		&tls.Config{Certificates: []tls.Certificate{server}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert, NextProtos: []string{gateway.ALPN, gateway.BondALPN}, MinVersion: tls.VersionTLS13},
		tls.Certificate{Certificate: [][]byte{leafDER, caDER}, PrivateKey: clientKey}
}

func sharedQUICTestListener(t *testing.T, echoTLS, vpnTLS *tls.Config) *sharedQUICListener {
	t.Helper()
	ln, err := newSharedQUICListener(context.Background(), "127.0.0.1:0", echoTLS, vpnTLS, &quic.Config{EnableDatagrams: true, MaxIncomingStreams: gateway.MaxFlows, MaxIncomingUniStreams: -1, HandshakeIdleTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func sharedQUICDial(t *testing.T, address string, protos []string, cert *tls.Certificate) *quic.Conn {
	t.Helper()
	cfg := &tls.Config{InsecureSkipVerify: true, NextProtos: protos, MinVersion: tls.VersionTLS13}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := quic.DialAddr(ctx, address, cfg, &quic.Config{EnableDatagrams: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseWithError(0, "test done") })
	return conn
}

func sharedQUICAccept(t *testing.T, ln interface {
	Accept(context.Context) (*quic.Conn, error)
}) *quic.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := ln.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func sharedQUICRejected(t *testing.T, address string, protos []string, cert *tls.Certificate, serverName string) {
	t.Helper()
	cfg := &tls.Config{InsecureSkipVerify: true, NextProtos: protos, MinVersion: tls.VersionTLS13, ServerName: serverName}
	// Return even an untrusted cert when the server advertises a different CA.
	if cert != nil {
		cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return cert, nil }
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := quic.DialAddr(ctx, address, cfg, &quic.Config{})
	if err != nil {
		return
	}
	defer c.CloseWithError(0, "test done")
	select {
	case <-c.Context().Done():
	case <-ctx.Done():
		t.Fatal("unauthorized connection remained usable")
	}
}

// Catches routing a mixed offer to echo or relaxing the VPN's client auth.
func TestSharedQUICAuthenticationIsolation(t *testing.T) {
	e, v, trusted := sharedQUICCertificates(t)
	ln := sharedQUICTestListener(t, e, v)
	for _, offer := range [][]string{{gateway.ALPN}, {gateway.BondALPN}, {protocol.ALPN, gateway.ALPN}, {gateway.ALPN, protocol.ALPN}, {protocol.ALPN, gateway.BondALPN}} {
		sharedQUICRejected(t, ln.Addr().String(), offer, nil, "")
	}
	_, _, untrusted := sharedQUICCertificates(t)
	sharedQUICRejected(t, ln.Addr().String(), []string{gateway.ALPN}, &untrusted, "")
	sharedQUICRejected(t, ln.Addr().String(), []string{"unrecognized/1"}, nil, "")
	for _, offer := range [][]string{{gateway.ALPN}, {gateway.BondALPN}, {protocol.ALPN, gateway.ALPN}} {
		c := sharedQUICDial(t, ln.Addr().String(), offer, &trusted)
		accepted := sharedQUICAccept(t, ln.VPN)
		if len(accepted.ConnectionState().TLS.VerifiedChains) == 0 {
			t.Fatal("VPN client not verified")
		}
		if c.ConnectionState().TLS.NegotiatedProtocol == protocol.ALPN {
			t.Fatal("mixed offer downgraded to echo")
		}
		accepted.CloseWithError(0, "finished")
	}
	c := sharedQUICDial(t, ln.Addr().String(), []string{protocol.ALPN}, nil)
	accepted := sharedQUICAccept(t, ln.Echo)
	if accepted.ConnectionState().TLS.NegotiatedProtocol != protocol.ALPN || len(accepted.ConnectionState().TLS.PeerCertificates) != 0 {
		t.Fatal("unauthenticated echo changed")
	}
	c.CloseWithError(0, "finished")
}

// Catches dropping configured SNI restrictions or managed device verification.
func TestSharedQUICPreservesVPNPolicy(t *testing.T) {
	e, v, client := sharedQUICCertificates(t)
	v = servertls.AllowServerNames(v, "vpn.example", []string{"cover.example"})
	var verified atomic.Int32
	v.VerifyConnection = func(tls.ConnectionState) error { verified.Add(1); return errors.New("device revoked") }
	ln := sharedQUICTestListener(t, e, v)
	sharedQUICRejected(t, ln.Addr().String(), []string{gateway.ALPN}, &client, "unknown.example")
	sharedQUICRejected(t, ln.Addr().String(), []string{gateway.ALPN}, &client, "cover.example")
	if verified.Load() != 1 {
		t.Fatalf("managed verification called %d times", verified.Load())
	}
	// Echo must remain available even when the VPN policy rejects that device.
	sharedQUICDial(t, ln.Addr().String(), []string{protocol.ALPN}, nil)
	sharedQUICAccept(t, ln.Echo)
}

// Catches callback-returned configs weakening the selected protocol's policy.
func TestSharedQUICCallbackCannotDowngradeVPN(t *testing.T) {
	e, v, _ := sharedQUICCertificates(t)
	unsafe := e.Clone()
	v.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) { return unsafe, nil }
	ln := sharedQUICTestListener(t, e, v)
	sharedQUICRejected(t, ln.Addr().String(), []string{protocol.ALPN, gateway.ALPN}, nil, "")
}

// Catches a blocked echo dispatch preventing authenticated VPN acceptance.
func TestSharedQUICBoundedDispatch(t *testing.T) {
	e, v, client := sharedQUICCertificates(t)
	ln := sharedQUICTestListener(t, e, v)
	var excess *quic.Conn
	for i := 0; i <= sharedQUICQueueSize; i++ {
		excess = sharedQUICDial(t, ln.Addr().String(), []string{protocol.ALPN}, nil)
	}
	select {
	case <-excess.Context().Done():
	case <-time.After(3 * time.Second):
		t.Fatal("full echo queue did not reject excess connection")
	}
	sharedQUICDial(t, ln.Addr().String(), []string{gateway.ALPN}, &client)
	sharedQUICAccept(t, ln.VPN)
}

// Catches shutdown leaving a routed connection or either accept loop alive.
func TestSharedQUICShutdown(t *testing.T) {
	e, v, client := sharedQUICCertificates(t)
	ctx, cancel := context.WithCancel(context.Background())
	ln, err := newSharedQUICListener(ctx, "127.0.0.1:0", e, v, &quic.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	echoClient := sharedQUICDial(t, ln.Addr().String(), []string{protocol.ALPN}, nil)
	sharedQUICAccept(t, ln.Echo)
	vpnClient := sharedQUICDial(t, ln.Addr().String(), []string{gateway.ALPN}, &client)
	sharedQUICAccept(t, ln.VPN)
	results := make(chan error, 2)
	go func() { _, err := ln.Echo.Accept(context.Background()); results <- err }()
	go func() { _, err := ln.VPN.Accept(context.Background()); results <- err }()
	cancel()
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err == nil {
				t.Fatal("closed listener accepted")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("accept loop did not stop")
		}
	}
	for _, c := range []*quic.Conn{echoClient, vpnClient} {
		select {
		case <-c.Context().Done():
		case <-time.After(3 * time.Second):
			t.Fatal("active connection not closed")
		}
	}
	closed := make(chan struct{})
	go func() { ln.Close(); ln.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not finish")
	}
}

// Catches interface changes breaking real echo/gateway handlers on shared or split listeners.
func TestSharedQUICHandlersAndSplitCompatibility(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared", true: "split"}[split], func(t *testing.T) {
			e, v, client := sharedQUICCertificates(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var echoLn echo.Listener
			var vpnLn gateway.QUICListener
			var echoAddr, vpnAddr string
			if split {
				el, err := quic.ListenAddr("127.0.0.1:0", e, &quic.Config{})
				if err != nil {
					t.Fatal(err)
				}
				defer el.Close()
				vl, err := quic.ListenAddr("127.0.0.1:0", v, &quic.Config{EnableDatagrams: true})
				if err != nil {
					t.Fatal(err)
				}
				defer vl.Close()
				echoLn, vpnLn, echoAddr, vpnAddr = el, vl, el.Addr().String(), vl.Addr().String()
			} else {
				ln := sharedQUICTestListener(t, e, v)
				echoLn, vpnLn, echoAddr, vpnAddr = ln.Echo, ln.VPN, ln.Addr().String(), ln.Addr().String()
			}
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			gw, err := gateway.New("127.0.0.0/8", log)
			if err != nil {
				t.Fatal(err)
			}
			results := make(chan error, 2)
			go func() { results <- echo.Serve(ctx, echoLn, log) }()
			go func() { results <- gw.ServeQUIC(ctx, vpnLn) }()
			for _, vpn := range []bool{false, true} {
				addr, offers := echoAddr, []string{protocol.ALPN}
				var cert *tls.Certificate
				if vpn {
					addr, offers, cert = vpnAddr, []string{gateway.ALPN}, &client
				}
				c := sharedQUICDial(t, addr, offers, cert)
				stream, err := c.OpenStreamSync(ctx)
				if err != nil {
					t.Fatal(err)
				}
				stream.SetDeadline(time.Now().Add(3 * time.Second))
				enc, dec := json.NewEncoder(stream), json.NewDecoder(stream)
				if vpn {
					if err := gateway.WriteJSON(stream, gateway.Request{Network: "echo"}); err != nil {
						t.Fatal(err)
					}
					var reply gateway.Reply
					if err := gateway.ReadJSON(stream, &reply); err != nil {
						t.Fatal(err)
					}
					if reply.Session == "" || reply.Error != "" {
						t.Fatalf("VPN echo reply: %+v", reply)
					}
				}
				if err := enc.Encode(protocol.Frame{Seq: 17}); err != nil {
					t.Fatal(err)
				}
				var frame protocol.Frame
				if err := dec.Decode(&frame); err != nil {
					t.Fatal(err)
				}
				if frame.Seq != 17 || frame.ConnectionID == "" {
					t.Fatalf("echo response: %+v", frame)
				}
			}
			cancel()
			for i := 0; i < 2; i++ {
				select {
				case <-results:
				case <-time.After(3 * time.Second):
					t.Fatal("handler did not stop")
				}
			}
		})
	}
}
