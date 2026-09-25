package mobile

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/xtaci/smux"
	"quiclab/internal/awg"
	"quiclab/internal/gateway"
	"quiclab/internal/protocol"
)

type gatewayConfig struct {
	DNS         string `json:"dns"`
	AWGConfig   string `json:"awg_config"`
	Transport   string `json:"transport"`
	Endpoint    string `json:"endpoint"`
	Hostname    string `json:"hostname"`
	Certificate string `json:"certificate"`
	Key         string `json:"key"`
	CA          string `json:"ca"`
}

// Gateway owns the proxy transport, separate from the existing echo experiment.
type Gateway struct {
	awg          *awg.Engine
	exitChecking bool
	mu           sync.Mutex
	q            *Client
	mux          *smux.Session
	cfg          gatewayConfig
	tls          *tls.Config
	sink         EventSink
	ctx          context.Context
	cancel       context.CancelFunc
	tunStop      func()
	session      int64
}

func NewGateway(sink EventSink) *Gateway { return &Gateway{sink: sink} }
func (g *Gateway) emit(kind string, v map[string]any) {
	if g.sink == nil {
		return
	}
	v["event"] = kind
	b, _ := json.Marshal(v)
	g.sink.OnEvent(string(b))
}
func (g *Gateway) Start(configJSON string, binder SocketBinder) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cancel != nil {
		return errors.New("gateway already running")
	}
	if e := json.Unmarshal([]byte(configJSON), &g.cfg); e != nil {
		return e
	}
	if g.cfg.Transport == "awg" {
		c, e := awg.Parse(g.cfg.AWGConfig)
		if e != nil {
			return e
		}
		if g.cfg.DNS != "" {
			if net.ParseIP(g.cfg.DNS).To4() == nil {
				return errors.New("IPv4 DNS required")
			}
			c.DNS = g.cfg.DNS
		}
		engine, e := awg.Start(c, g.cfg.Endpoint, binder)
		if e != nil {
			return e
		}
		g.awg = engine
		g.ctx, g.cancel = context.WithCancel(context.Background())
		g.session++
		g.emit("connected", map[string]any{"transport": "awg", "session": g.session, "detail": "AWG engine ready; waiting for tunnel probe"})
		go g.awgHeartbeat(g.ctx, engine, g.cfg.Endpoint)
		return nil
	}
	if g.cfg.Transport != "quic" && g.cfg.Transport != "https" {
		return errors.New("choose quic or https")
	}
	host, _, e := net.SplitHostPort(g.cfg.Endpoint)
	if e != nil || net.ParseIP(host).To4() == nil {
		return errors.New("numeric IPv4 endpoint required")
	}
	pair, e := tls.X509KeyPair([]byte(g.cfg.Certificate), []byte(g.cfg.Key))
	if e != nil {
		return fmt.Errorf("client identity: %w", e)
	}
	g.tls = &tls.Config{ServerName: g.cfg.Hostname, Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
	if g.cfg.CA != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(g.cfg.CA)) {
			return errors.New("invalid server CA")
		}
		g.tls.RootCAs = pool
	}
	if e = g.connect(binder); e != nil {
		return e
	}
	return nil
}
func (g *Gateway) connect(binder SocketBinder) error {
	g.ctx, g.cancel = context.WithCancel(context.Background())
	if g.cfg.Transport == "quic" {
		g.q = NewClient(g.sink)
		g.q.gatewayTLS = g.tls.Clone()
		g.q.gatewayTLS.NextProtos = []string{gateway.ALPN}
		if e := g.q.Start(g.cfg.Endpoint, g.cfg.Hostname, "", 100, binder); e != nil {
			g.cancel()
			g.cancel = nil
			g.q = nil
			return e
		}
		return nil
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	if binder != nil {
		dialer.Control = func(_, _ string, raw syscall.RawConn) error {
			var e error
			err := raw.Control(func(fd uintptr) { e = binder.Bind(int64(fd)) })
			if err != nil {
				return err
			}
			return e
		}
	}
	tr := &http.Transport{TLSClientConfig: g.tls.Clone(), DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp4", g.cfg.Endpoint)
	}}
	defer tr.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(g.ctx, 10*time.Second)
	defer cancel()
	_, port, _ := net.SplitHostPort(g.cfg.Endpoint)
	ws, _, e := websocket.Dial(ctx, "wss://"+net.JoinHostPort(g.cfg.Hostname, port)+"/tunnel", &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}})
	if e != nil {
		g.cancel()
		g.cancel = nil
		return e
	}
	ws.SetReadLimit(1 << 20)
	m, e := smux.Client(websocket.NetConn(g.ctx, ws, websocket.MessageBinary), gateway.MuxConfig())
	if e != nil {
		ws.CloseNow()
		g.cancel()
		g.cancel = nil
		return e
	}
	g.mux = m
	g.session++
	s, e := gateway.Open(g.ctx, func(context.Context) (gateway.Stream, error) {
		v, e := m.OpenStream()
		if e != nil {
			return nil, e
		}
		return gateway.NewMStream(v), nil
	}, "echo", "")
	if e != nil {
		m.Close()
		g.cancel()
		g.cancel = nil
		return e
	}
	g.emit("connected", map[string]any{"transport": "https", "session": g.session})
	go g.heartbeat(g.ctx, m, s)
	return nil
}
func (g *Gateway) heartbeat(ctx context.Context, m *smux.Session, s gateway.Stream) {
	defer s.Close()
	start := time.Now()
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		var seq uint64
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			seq++
			s.SetDeadline(time.Now().Add(15 * time.Second))
			if json.NewEncoder(s).Encode(protocol.Frame{Seq: seq, SentNS: time.Since(start).Nanoseconds()}) != nil {
				return
			}
		}
	}()
	scan := bufio.NewScanner(s)
	scan.Buffer(make([]byte, 1024), 4096)
	for scan.Scan() {
		var f protocol.Frame
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			break
		}
		g.emit("echo", map[string]any{"rtt_ms": float64(time.Since(start).Nanoseconds()-f.SentNS) / 1e6, "connection_id": f.ConnectionID})
	}
	m.Close()
	if ctx.Err() == nil {
		g.emit("disconnected", map[string]any{"detail": "HTTPS tunnel closed; existing flows ended"})
	}
}
func (g *Gateway) open(ctx context.Context) (gateway.Stream, error) {
	g.mu.Lock()
	q, m := g.q, g.mux
	g.mu.Unlock()
	if q != nil {
		q.op.Lock()
		c := q.conn
		q.op.Unlock()
		if c == nil {
			return nil, errors.New("QUIC disconnected")
		}
		s, e := c.OpenStreamSync(ctx)
		if e != nil {
			return nil, e
		}
		return gateway.QStream{Stream: s}, nil
	}
	if m != nil {
		s, e := m.OpenStream()
		if e != nil {
			return nil, e
		}
		return gateway.NewMStream(s), nil
	}
	return nil, errors.New("gateway disconnected")
}
func (g *Gateway) PreparePath(key string, binder SocketBinder) error {
	g.mu.Lock()
	if g.awg != nil {
		g.mu.Unlock()
		return errors.New("AWG does not probe standby paths")
	}
	q := g.q
	cfg := g.cfg
	tc := g.tls.Clone()
	g.mu.Unlock()
	if q != nil {
		return q.PreparePath(key, binder)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	d := &net.Dialer{}
	if binder != nil {
		d.Control = func(_, _ string, raw syscall.RawConn) error {
			var e error
			err := raw.Control(func(fd uintptr) { e = binder.Bind(int64(fd)) })
			if err != nil {
				return err
			}
			return e
		}
	}
	started := time.Now()
	c, e := (&tls.Dialer{NetDialer: d, Config: tc}).DialContext(ctx, "tcp4", cfg.Endpoint)
	if e != nil {
		return e
	}
	c.Close()
	g.emit("standby_ready", map[string]any{"probe_ms": float64(time.Since(started)) / float64(time.Millisecond), "key": key})
	return nil
}
func (g *Gateway) MigrateTo(key string, binder SocketBinder) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.awg != nil {
		return g.awg.Migrate(binder)
	}
	if g.q != nil {
		return g.q.MigrateTo(key, binder)
	}
	if g.cancel != nil {
		g.cancel()
	}
	if g.mux != nil {
		g.mux.Close()
		g.mux = nil
	}
	g.cancel = nil
	return g.connect(binder)
}
func (g *Gateway) InvalidatePath(key string) {
	g.mu.Lock()
	q := g.q
	g.mu.Unlock()
	if q != nil {
		q.InvalidatePath(key)
	}
}
func (g *Gateway) LocalAddress() string {
	g.mu.Lock()
	q := g.q
	isAWG := g.awg != nil
	g.mu.Unlock()
	if isAWG {
		return "awg"
	}
	if q != nil {
		return q.LocalAddress()
	}
	return "https"
}
func (g *Gateway) Stop() {
	g.mu.Lock()
	stop := g.tunStop
	g.tunStop = nil
	if g.cancel != nil {
		g.cancel()
		g.cancel = nil
	}
	if g.q != nil {
		g.q.Stop()
		g.q = nil
	}
	if g.mux != nil {
		g.mux.Close()
		g.mux = nil
	}
	a := g.awg
	g.awg = nil
	g.mu.Unlock()
	if a != nil {
		a.Close()
	}
	if stop != nil {
		stop()
	}
}
