package vless

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/xtls/xray-core/app/dispatcher"
	"github.com/xtls/xray-core/app/proxyman"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	"github.com/xtls/xray-core/common"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/proxy/freedom"
	account "github.com/xtls/xray-core/proxy/vless"
	inbound "github.com/xtls/xray-core/proxy/vless/inbound"
	outbound "github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/transport/internet"
	xtls "github.com/xtls/xray-core/transport/internet/tls"
	"google.golang.org/protobuf/types/known/emptypb"
)

func fixtureCertificate(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture.invalid"}, DNSNames: []string{"fixture.invalid"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: raw})
}
func startTLSFixture(t *testing.T) (uint32, []byte, *engine) {
	t.Helper()
	cert, key := fixtureCertificate(t)
	portListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint32(portListener.Addr().(*net.TCPAddr).Port)
	portListener.Close()
	sec := serial.ToTypedMessage(&xtls.Config{Certificate: []*xtls.Certificate{{Certificate: cert, Key: key, OneTimeLoading: true}}})
	cfg := &core.Config{App: []*serial.TypedMessage{serial.ToTypedMessage(&emptypb.Empty{}), serial.ToTypedMessage(&proxyman.OutboundConfig{}), serial.ToTypedMessage(&proxyman.InboundConfig{})},
		Inbound:  []*core.InboundHandlerConfig{{Tag: "fixture", ReceiverSettings: serial.ToTypedMessage(&proxyman.ReceiverConfig{Listen: xnet.NewIPOrDomain(xnet.LocalHostIP), PortList: &xnet.PortList{Range: []*xnet.PortRange{{From: port, To: port}}}, StreamSettings: &internet.StreamConfig{ProtocolName: "tcp", SecurityType: sec.Type, SecuritySettings: []*serial.TypedMessage{sec}}}), ProxySettings: serial.ToTypedMessage(&inbound.Config{Decryption: "none", Clients: []*protocol.User{{Email: "fixture", Account: serial.ToTypedMessage(&account.Account{Id: "11111111-1111-4111-8111-111111111111", Encryption: "none"})}}})}},
		Outbound: []*core.OutboundHandlerConfig{{ProxySettings: serial.ToTypedMessage(&freedom.Config{})}}}
	server, err := newEngine(context.Background(), cfg, &net.Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	return port, cert, server
}
func clientTLSFixture(port uint32, cert []byte, wrong bool) *core.Config {
	cfg := fixtureConfig()
	sender, _ := cfg.Outbound[0].SenderSettings.GetInstance()
	s := sender.(*proxyman.SenderConfig)
	sec := serial.ToTypedMessage(&xtls.Config{ServerName: "fixture.invalid", DisableSystemRoot: true, Certificate: []*xtls.Certificate{{Certificate: cert, Usage: xtls.Certificate_AUTHORITY_VERIFY}}})
	s.StreamSettings.SecuritySettings = []*serial.TypedMessage{sec}
	cfg.Outbound[0].SenderSettings = serial.ToTypedMessage(s)
	raw, _ := cfg.Outbound[0].ProxySettings.GetInstance()
	v := raw.(*outbound.Config)
	v.Vnext.Address = xnet.NewIPOrDomain(xnet.LocalHostIP)
	v.Vnext.Port = port
	if wrong {
		v.Vnext.User.Account = serial.ToTypedMessage(&account.Account{Id: "22222222-2222-4222-8222-222222222222", Encryption: "none"})
	}
	cfg.Outbound[0].ProxySettings = serial.ToTypedMessage(v)
	return cfg
}
func TestEngineTLSRoundTripAndWrongIdentity(t *testing.T) {
	port, cert, _ := startTLSFixture(t)
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, e := echo.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	for _, wrong := range []bool{false, true} {
		t.Run(fmt.Sprint(wrong), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			e, err := newEngine(ctx, clientTLSFixture(port, cert, wrong), &net.Dialer{})
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			c, err := e.DialContext(ctx, "tcp4", echo.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			result := make(chan error, 1)
			go func() {
				if _, err := c.Write([]byte("vless-fixture")); err != nil {
					result <- err
					return
				}
				b := make([]byte, 13)
				_, err := io.ReadFull(c, b)
				if err == nil && string(b) != "vless-fixture" {
					err = fmt.Errorf("payload mismatch")
				}
				result <- err
			}()
			select {
			case err := <-result:
				if wrong && err == nil {
					t.Fatal("wrong identity accepted")
				}
				if !wrong && err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("flow did not finish within bound")
			}
		})
	}
}
func TestServerRevokeRealTLSFlow(t *testing.T) {
	port, cert, server := startTLSFixture(t)
	guard, ok := server.instance.GetFeature(routing.DispatcherType()).(*deviceDispatcher)
	if !ok {
		t.Fatal("server admission dispatcher missing")
	}
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, e := echo.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	e, err := newEngine(ctx, clientTLSFixture(port, cert, false), &net.Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	c, err := e.DialContext(ctx, "tcp4", echo.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 1)
	if _, err = io.ReadFull(c, b); err != nil {
		t.Fatal(err)
	}
	guard.Revoke("11111111-1111-4111-8111-111111111111")
	done := make(chan error, 1)
	go func() { _, err := c.Read(b); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revoked flow readable")
		}
	case <-ctx.Done():
		t.Fatal("revoked TLS flow not closed")
	}
	// Retained upstream credentials must not bypass the revoked-state gate.
	again, err := e.DialContext(ctx, "tcp4", echo.Addr().String())
	if err != nil {
		return
	}
	defer again.Close()
	rejected := make(chan error, 1)
	go func() { again.Write([]byte("b")); _, err := again.Read(b); rejected <- err }()
	select {
	case err := <-rejected:
		if err == nil {
			t.Fatal("revoked user admitted")
		}
	case <-ctx.Done():
		t.Fatal("revoked admission hung")
	}
}

// Test-only protobuf registration; the server product will own its config type.
func init() {
	common.Must(common.RegisterConfig((*emptypb.Empty)(nil), func(ctx context.Context, _ interface{}) (interface{}, error) {
		base, err := common.CreateObject(ctx, &dispatcher.Config{})
		if err != nil {
			return nil, err
		}
		return newDeviceDispatcher(base.(routing.Dispatcher)), nil
	}))
}

func TestServerDispatchPreservesFinalResponse(t *testing.T) {
	port, cert, _ := startTLSFixture(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	payload := bytes.Repeat([]byte("final-response"), 65536)
	go func() {
		c, err := listener.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		b := make([]byte, 1)
		io.ReadFull(c, b)
		c.Write(payload)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	e, err := newEngine(ctx, clientTLSFixture(port, cert, false), &net.Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	c, err := e.DialContext(ctx, "tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	done := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(c); done <- b }()
	select {
	case b := <-done:
		if !bytes.Equal(b, payload) {
			t.Fatalf("final response truncated: got %d want %d", len(b), len(payload))
		}
	case <-ctx.Done():
		t.Fatal("response did not finish")
	}
}
