package vless

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/xtls/xray-core/app/dispatcher"
	"github.com/xtls/xray-core/app/proxyman"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	account "github.com/xtls/xray-core/proxy/vless"
	outbound "github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/transport/internet"
	xtls "github.com/xtls/xray-core/transport/internet/tls"
)

func fixtureConfig() *core.Config {
	security := serial.ToTypedMessage(&xtls.Config{ServerName: "fixture.invalid"})
	return &core.Config{App: []*serial.TypedMessage{serial.ToTypedMessage(&dispatcher.Config{}), serial.ToTypedMessage(&proxyman.OutboundConfig{})}, Outbound: []*core.OutboundHandlerConfig{{
		SenderSettings: serial.ToTypedMessage(&proxyman.SenderConfig{StreamSettings: &internet.StreamConfig{ProtocolName: "tcp", SecurityType: security.Type, SecuritySettings: []*serial.TypedMessage{security}}}),
		ProxySettings:  serial.ToTypedMessage(&outbound.Config{Vnext: &protocol.ServerEndpoint{Address: xnet.NewIPOrDomain(xnet.DomainAddress("fixture.invalid")), Port: 443, User: &protocol.User{Account: serial.ToTypedMessage(&account.Account{Id: "11111111-1111-4111-8111-111111111111", Encryption: "none"})}}}),
	}}}
}
func TestEngineRoutesRealCoreInstances(t *testing.T) {
	var ready sync.WaitGroup
	ready.Add(2)
	for _, name := range []string{"wifi", "cell"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			called := make(chan string, 10)
			e, err := newEngine(context.Background(), fixtureConfig(), factoryFunc(func(ctx context.Context, n, a string) (net.Conn, error) {
				select {
				case called <- n + " " + a:
				default:
				}
				return nil, errors.New("binding rejected")
			}))
			if err != nil {
				ready.Done()
				t.Fatal(err)
			}
			defer e.Close()
			ready.Done()
			ready.Wait()
			c, err := e.DialContext(context.Background(), "tcp4", "target.invalid:80")
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			select {
			case actual := <-called:
				if actual != "tcp4 fixture.invalid:443" {
					t.Fatalf("unexpected endpoint %s", actual)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("core bypassed per-instance factory")
			}
		})
	}
}
func TestEngineCloseCancelsRealCoreDial(t *testing.T) {
	called := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	e, err := newEngine(context.Background(), fixtureConfig(), factoryFunc(func(ctx context.Context, n, a string) (net.Conn, error) {
		select {
		case called <- struct{}{}:
		default:
		}
		<-ctx.Done()
		select {
		case cancelled <- struct{}{}:
		default:
		}
		return nil, ctx.Err()
	}))
	if err != nil {
		t.Fatal(err)
	}
	c, err := e.DialContext(context.Background(), "tcp4", "target.invalid:80")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("dial not started")
	}
	e.Close()
	e.Close()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("dial leaked")
	}
	if _, err = e.DialContext(context.Background(), "tcp4", "target.invalid:80"); err == nil {
		t.Fatal("closed engine accepted new flow")
	}
}
func TestNewRestrictedClient(t *testing.T) {
	c := validConfig()
	e, err := New(context.Background(), c, factoryFunc(func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("not used") }))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	c.UUID = "invalid"
	if bad, err := New(context.Background(), c, factoryFunc(nil)); err == nil || bad != nil {
		t.Fatal("invalid configuration admitted")
	}
}

func TestEngineUnsupportedUDPDoesNotOpenSocket(t *testing.T) {
	calls := 0
	e, err := New(context.Background(), validConfig(), factoryFunc(func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("unexpected dial")
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if _, err = e.DialContext(context.Background(), "udp4", "127.0.0.1:53"); err == nil {
		t.Fatal("unsupported UDP accepted")
	}
	if calls != 0 {
		t.Fatal("unsupported UDP opened a socket")
	}
}
