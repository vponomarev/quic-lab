package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
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

func newSNIRouter(parent context.Context, ln net.Listener, host, fallback string) (net.Listener, error) {
	h, _, e := net.SplitHostPort(fallback)
	if e != nil || net.ParseIP(h) == nil || !net.ParseIP(h).IsLoopback() {
		return nil, errors.New("TLS fallback must use a literal loopback IP")
	}
	if host == "" || strings.ContainsAny(host, "/ :") {
		return nil, errors.New("invalid TLS host")
	}
	ctx, cancel := context.WithCancel(parent)
	r := &sniRouter{Listener: ln, ctx: ctx, cancel: cancel, local: make(chan net.Conn)}
	go func() { <-ctx.Done(); r.Close() }()
	go r.route(strings.ToLower(host), fallback)
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
func (r *sniRouter) route(host, fallback string) {
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
			if name == host {
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
			backend, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(r.ctx, "tcp", fallback)
			if e != nil {
				return
			}
			defer backend.Close()
			stopBackend := context.AfterFunc(r.ctx, func() { backend.Close() })
			defer stopBackend()
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
