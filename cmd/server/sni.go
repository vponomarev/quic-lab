package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	proxyproto "github.com/pires/go-proxyproto"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// Inspect ClientHello with Go's TLS parser, without writing TLS data to the peer.
// Replay every consumed byte to either the local TLS stack or the fallback.
type helloProbe struct {
	net.Conn
	data      bytes.Buffer
	remaining int
}

func (c *helloProbe) Read(p []byte) (int, error) {
	if c.remaining <= 0 {
		return 0, errors.New("ClientHello exceeds 64 KiB")
	}
	if len(p) > c.remaining {
		p = p[:c.remaining]
	}
	n, e := c.Conn.Read(p)
	c.remaining -= n
	c.data.Write(p[:n])
	return n, e
}
func (c *helloProbe) Write(p []byte) (int, error) { return len(p), nil }

type replayConn struct {
	net.Conn
	reader io.Reader
}

func (c *replayConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func readSNI(c net.Conn) (string, net.Conn, error) {
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	probe := &helloProbe{Conn: c, remaining: 65536}
	host := ""
	seen := false
	stop := errors.New("inspection complete")
	tc := tls.Server(probe, &tls.Config{GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) { host = h.ServerName; seen = true; return nil, stop }})
	_ = tc.Handshake()
	if !seen {
		return "", nil, errors.New("invalid or incomplete ClientHello")
	}
	_ = c.SetDeadline(time.Time{})
	return strings.ToLower(host), &replayConn{c, io.MultiReader(bytes.NewReader(probe.data.Bytes()), c)}, nil
}

type sniRouter struct {
	net.Listener
	ctx    context.Context
	cancel context.CancelFunc
	local  chan net.Conn
	once   sync.Once
}

func newSNIRouter(parent context.Context, ln net.Listener, host, fallback string, allowedNames ...string) (net.Listener, error) {
	return newSNIRouterWithRoutes(parent, ln, host, fallback, nil, allowedNames...)
}
func newSNIRouterWithRoutes(parent context.Context, ln net.Listener, host, fallback string, routes []sniRoute, allowedNames ...string) (net.Listener, error) {
	targets, err := validateSNIRoutes(host, allowedNames, routes)
	if err != nil {
		return nil, err
	}
	if fallback != "" {
		if err := validateAddress(fallback, true); err != nil {
			return nil, err
		}
	}
	for _, route := range targets {
		if routeTargetsListener(route.Target, ln.Addr().String()) {
			return nil, errors.New("TLS route targets its listener")
		}
	}
	if fallback != "" && routeTargetsListener(fallback, ln.Addr().String()) {
		return nil, errors.New("TLS fallback targets its listener")
	}
	if host == "" || strings.ContainsAny(host, "/ :") {
		return nil, errors.New("invalid TLS host")
	}
	names := map[string]bool{strings.ToLower(host): true}
	for _, name := range allowedNames {
		if name == "" || strings.ContainsAny(name, "/ :*\x5c\r\n\t") {
			return nil, errors.New("invalid VPN SNI name")
		}
		names[strings.ToLower(name)] = true
	}
	ctx, cancel := context.WithCancel(parent)
	r := &sniRouter{Listener: ln, ctx: ctx, cancel: cancel, local: make(chan net.Conn)}
	go func() { <-ctx.Done(); r.Close() }()
	go r.route(names, fallback, targets)
	return r, nil
}
func (r *sniRouter) Close() error {
	var e error
	r.once.Do(func() { r.cancel(); e = r.Listener.Close() })
	return e
}
func (r *sniRouter) Accept() (net.Conn, error) {
	select {
	case c := <-r.local:
		return c, nil
	case <-r.ctx.Done():
		return nil, net.ErrClosed
	}
}
func (r *sniRouter) route(names map[string]bool, fallback string, targets map[string]sniRoute) {
	slots := make(chan struct{}, 1024)
	for {
		c, e := r.Listener.Accept()
		if e != nil {
			r.Close()
			return
		}
		select {
		case slots <- struct{}{}:
		default:
			c.Close()
			continue
		}
		go func() {
			defer func() { <-slots }()
			stop := context.AfterFunc(r.ctx, func() { c.Close() })
			name, replay, e := readSNI(c)
			if e != nil {
				stop()
				c.Close()
				return
			}
			if names[name] {
				select {
				case r.local <- replay:
					stop()
				case <-r.ctx.Done():
					stop()
					c.Close()
				}
				return
			}
			defer stop()
			defer c.Close()
			target := sniRoute{Target: fallback}
			if explicit, ok := targets[name]; ok {
				target = explicit
			}
			if target.Target == "" {
				return
			}
			backend, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(r.ctx, "tcp", target.Target)
			if e != nil {
				return
			}
			defer backend.Close()
			stopBackend := context.AfterFunc(r.ctx, func() { backend.Close() })
			defer stopBackend()
			if target.ProxyProtocol {
				_ = backend.SetWriteDeadline(time.Now().Add(5 * time.Second))
				header := proxyproto.HeaderProxyFromAddrs(2, c.RemoteAddr(), c.LocalAddr())
				if header.Command != proxyproto.PROXY {
					return
				}
				if _, err := header.WriteTo(backend); err != nil {
					return
				}
				_ = backend.SetWriteDeadline(time.Time{})
			}
			done := make(chan struct{})
			go func() {
				io.Copy(backend, replay)
				if t, ok := backend.(*net.TCPConn); ok {
					t.CloseWrite()
				}
				close(done)
			}()
			io.Copy(c, backend)
			c.Close()
			backend.Close()
			<-done
		}()
	}
}
