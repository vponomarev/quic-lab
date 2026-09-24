package gateway

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/quic-go/quic-go"
	"github.com/xtaci/smux"
	"quiclab/internal/protocol"
)

type Server struct {
	Allowed []netip.Prefix
	Log     *slog.Logger
	slots   chan struct{}
}

func New(allow string, log *slog.Logger) (*Server, error) {
	s := &Server{Log: log, slots: make(chan struct{}, 1024)}
	for _, v := range strings.Split(allow, ",") {
		p, e := netip.ParsePrefix(strings.TrimSpace(v))
		if e != nil || !p.Addr().Is4() {
			return nil, fmt.Errorf("invalid IPv4 allow prefix %q", v)
		}
		s.Allowed = append(s.Allowed, p.Masked())
	}
	return s, nil
}
func (s *Server) Permitted(address string) bool {
	host, p, e := net.SplitHostPort(address)
	if e != nil {
		return false
	}
	port, e := strconv.Atoi(p)
	if e != nil || port < 1 || port > 65535 {
		return false
	}
	ip, e := netip.ParseAddr(host)
	if e != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() || ip == netip.MustParseAddr("255.255.255.255") {
		return false
	}
	for _, p := range s.Allowed {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
func TLS(cert tls.Certificate, caFile string) (*tls.Config, error) {
	b, e := os.ReadFile(caFile)
	if e != nil {
		return nil, e
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(b) {
		return nil, errors.New("invalid client CA")
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13, NextProtos: []string{ALPN}}, nil
}
func identity() string { var b [16]byte; rand.Read(b[:]); return hex.EncodeToString(b[:]) }
func (s *Server) ServeQUIC(ctx context.Context, ln *quic.Listener) error {
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		c, e := ln.Accept(ctx)
		if e != nil {
			return e
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer c.CloseWithError(0, "gateway closed")
			stop := context.AfterFunc(ctx, func() { c.CloseWithError(0, "server stopped") })
			defer stop()
			s.serve(c.Context(), "quic", func() (Stream, error) {
				v, e := c.AcceptStream(c.Context())
				if e != nil {
					return nil, e
				}
				return QStream{v}, nil
			}, func() { c.CloseWithError(1, "closed") })
		}()
	}
}
func (s *Server) WebSocket(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
		http.Error(w, "client certificate required", 403)
		return
	}
	c, e := websocket.Accept(w, r, nil)
	if e != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	m, e := smux.Server(websocket.NetConn(ctx, c, websocket.MessageBinary), MuxConfig())
	if e != nil {
		return
	}
	defer m.Close()
	s.serve(ctx, "https", func() (Stream, error) {
		v, e := m.AcceptStream()
		if e != nil {
			return nil, e
		}
		return NewMStream(v), nil
	}, func() { m.Close() })
}
func (s *Server) serve(ctx context.Context, transport string, accept func() (Stream, error), closeSession func()) {
	id := identity()
	log := s.Log.With("session", id, "transport", transport)
	log.Info("gateway_open")
	defer log.Info("gateway_closed")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, closeSession)
	defer stop()
	slots := make(chan struct{}, MaxFlows)
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	for {
		st, e := accept()
		if e != nil {
			return
		}
		select {
		case slots <- struct{}{}:
		default:
			st.Close()
			continue
		}
		select {
		case s.slots <- struct{}{}:
		default:
			<-slots
			st.Close()
			continue
		}
		wg.Add(1)
		go func() { defer wg.Done(); defer func() { <-slots; <-s.slots }(); s.handle(ctx, st, id, log) }()
	}
}
func (s *Server) handle(ctx context.Context, st Stream, id string, log *slog.Logger) {
	defer st.Close()
	stop := context.AfterFunc(ctx, func() { st.Close() })
	defer stop()
	st.SetDeadline(time.Now().Add(10 * time.Second))
	var req Request
	if ReadJSON(st, &req) != nil {
		return
	}
	if req.Network == "echo" {
		if WriteJSON(st, Reply{Session: id}) != nil {
			return
		}
		scan := bufio.NewScanner(st)
		scan.Buffer(make([]byte, 1024), 4096)
		for {
			st.SetDeadline(time.Now().Add(90 * time.Second))
			if !scan.Scan() {
				return
			}
			var f protocol.Frame
			if json.Unmarshal(scan.Bytes(), &f) != nil {
				return
			}
			f.ConnectionID = id
			if json.NewEncoder(st).Encode(f) != nil {
				return
			}
		}
	}
	if req.Network != "tcp" && req.Network != "dns" {
		WriteJSON(st, Reply{Error: "unsupported network"})
		return
	}
	if !s.Permitted(req.Address) {
		WriteJSON(st, Reply{Error: "destination denied"})
		return
	}
	if req.Network == "dns" {
		_, p, _ := net.SplitHostPort(req.Address)
		if p != "53" {
			WriteJSON(st, Reply{Error: "DNS port must be 53"})
			return
		}
	}
	dctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	network := "tcp4"
	if req.Network == "dns" {
		network = "udp4"
	}
	dst, e := (&net.Dialer{}).DialContext(dctx, network, req.Address)
	if e != nil {
		WriteJSON(st, Reply{Error: "destination unavailable"})
		return
	}
	defer dst.Close()
	if WriteJSON(st, Reply{Session: id}) != nil {
		return
	}
	st.SetDeadline(time.Time{})
	flow := identity()
	log.Info("flow_open", "flow", flow, "network", req.Network, "destination", req.Address)
	defer log.Info("flow_closed", "flow", flow)
	if req.Network == "tcp" {
		up, down := Relay(ctx, st, dst)
		log.Info("flow_bytes", "flow", flow, "up", up, "down", down)
		return
	}
	for {
		st.SetDeadline(time.Now().Add(30 * time.Second))
		q, e := ReadPacket(st, 4096)
		if e != nil {
			return
		}
		dst.SetDeadline(time.Now().Add(5 * time.Second))
		if _, e = dst.Write(q); e != nil {
			return
		}
		b := make([]byte, 4096)
		n, e := dst.Read(b)
		if e != nil {
			return
		}
		if WritePacket(st, b[:n]) != nil {
			return
		}
	}
}
