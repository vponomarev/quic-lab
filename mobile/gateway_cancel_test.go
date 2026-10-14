package mobile

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"github.com/quic-go/quic-go"
	"io"
	"log/slog"
	"net"
	"quiclab/internal/gateway"
	"strings"
	"testing"
	"time"
)

func TestGatewayStartContextCancelsUnresponsivePeer(t *testing.T) {
	_, cp, kp := testIdentity(t)
	for _, mode := range []string{"quic", "https"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reached := make(chan struct{})
			peerRelease := make(chan struct{})
			defer close(peerRelease)
			var endpoint string
			if mode == "quic" {
				pc, e := net.ListenPacket("udp4", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				defer pc.Close()
				endpoint = pc.LocalAddr().String()
				go func() {
					b := make([]byte, 2048)
					if _, _, e := pc.ReadFrom(b); e == nil {
						close(reached)
					}
				}()
			} else {
				ln, e := net.Listen("tcp4", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				defer ln.Close()
				endpoint = ln.Addr().String()
				go func() {
					c, e := ln.Accept()
					if e == nil {
						defer c.Close()
						close(reached)
						<-peerRelease
					}
				}()
			}
			raw, _ := json.Marshal(gatewayConfig{Transport: mode, Endpoint: endpoint, Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
			g := NewGateway(nil)
			done := make(chan error, 1)
			go func() { done <- g.startWithContext(ctx, string(raw), nil) }()
			select {
			case <-reached:
			case <-time.After(3 * time.Second):
				cancel()
				t.Fatal("attempt did not reach peer")
			}
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancelled start succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("startup ignored cancellation")
			}
			g.Stop()
		})
	}
}

func TestGatewayStartContextCancelsEchoNegotiation(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(cp))
	ln, err := quic.ListenAddr("127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, NextProtos: []string{gateway.ALPN}}, &quic.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reached := make(chan struct{})
	peerRelease := make(chan struct{})
	defer close(peerRelease)
	go func() {
		c, e := ln.Accept(ctx)
		if e != nil {
			return
		}
		defer c.CloseWithError(0, "")
		stream, e := c.AcceptStream(ctx)
		if e != nil {
			return
		}
		var req gateway.Request
		if gateway.ReadJSON(stream, &req) == nil {
			close(reached)
			<-peerRelease
		}
	}()
	raw, _ := json.Marshal(gatewayConfig{Transport: "quic", Endpoint: ln.Addr().String(), Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
	g := NewGateway(nil)
	done := make(chan error, 1)
	go func() { done <- g.startWithContext(ctx, string(raw), nil) }()
	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("echo request missing")
	}
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled echo succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("echo ignored cancellation")
	}
	g.Stop()
}

type connectedBlockSink struct {
	entered chan struct{}
	release chan struct{}
}

func (s *connectedBlockSink) OnEvent(raw string) {
	if strings.Contains(raw, `"event":"connected"`) {
		close(s.entered)
		<-s.release
	}
}
func TestClientStartupCancellationCallbackDisarmedBeforeConnectedEvent(t *testing.T) {
	pair, cp, _ := testIdentity(t)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(cp))
	tc := &tls.Config{Certificates: []tls.Certificate{pair}, RootCAs: roots, NextProtos: []string{gateway.ALPN}, ServerName: "localhost"}
	ln, err := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	serverCtx, stopServer := context.WithCancel(context.Background())
	defer stopServer()
	srv, _ := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	go srv.ServeQUIC(serverCtx, ln)
	sink := &connectedBlockSink{make(chan struct{}), make(chan struct{})}
	c := NewClient(sink)
	c.gatewayTLS = tc
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.startWithContext(ctx, ln.Addr().String(), "localhost", "", 100, nil) }()
	select {
	case <-sink.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("no connected event")
	}
	cancel()
	time.Sleep(30 * time.Millisecond)
	close(sink.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	if c.conn.Context().Err() != nil {
		t.Fatal("startup callback closed published connection")
	}
}
