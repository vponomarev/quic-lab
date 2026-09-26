package mobile

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"quiclab/internal/protocol"
)

type WebSocketClient struct {
	op            sync.Mutex
	events        sync.Mutex
	sink          EventSink
	cancel        context.CancelFunc
	conn          *websocket.Conn
	httpTransport *http.Transport
	wg            sync.WaitGroup
}

// Permit normal TLS resumption across TCP reconnects; do not force a full
// handshake merely to make the comparison favor QUIC.
var websocketTLSCache = tls.NewLRUClientSessionCache(32)

func NewWebSocketClient(sink EventSink) *WebSocketClient { return &WebSocketClient{sink: sink} }
func (c *WebSocketClient) emit(kind string, fields map[string]any) {
	if c.sink == nil {
		return
	}
	fields["event"] = kind
	fields["time"] = time.Now().UTC().Format(time.RFC3339Nano)
	b, _ := json.Marshal(fields)
	c.events.Lock()
	defer c.events.Unlock()
	c.sink.OnEvent(string(b))
}

// Start opens WSS over HTTP/1.1 TLS/TCP, bound to the selected Android Network.
// numericEndpoint is resolved through that same Network before calling Start.
func (c *WebSocketClient) Start(numericEndpoint, hostname string, intervalMS int, binder SocketBinder) error {
	c.op.Lock()
	defer c.op.Unlock()
	if c.conn != nil {
		return errors.New("stop previous WebSocket first")
	}
	host, _, err := net.SplitHostPort(numericEndpoint)
	if err != nil || net.ParseIP(host) == nil || hostname == "" {
		return errors.New("numeric endpoint and TLS hostname required")
	}
	if intervalMS < 10 || intervalMS > 5000 {
		return errors.New("interval must be 10..5000 ms")
	}
	tlsConfig, err := clientTLS(hostname, "")
	if err != nil {
		return err
	}
	tlsConfig.NextProtos = []string{"http/1.1"}
	tlsConfig.ClientSessionCache = websocketTLSCache
	dialer := net.Dialer{Timeout: 5 * time.Second}
	if binder != nil {
		dialer.Control = func(_, _ string, raw syscall.RawConn) error {
			var bindErr error
			err := raw.Control(func(fd uintptr) { bindErr = binder.Bind(int64(fd)) })
			if err != nil {
				return err
			}
			return bindErr
		}
	}
	tr := &http.Transport{TLSClientConfig: tlsConfig, ForceAttemptHTTP2: false,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", numericEndpoint)
		}}
	ctx, cancel := context.WithCancel(context.Background())
	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	started := time.Now()
	conn, resp, err := websocket.Dial(dialCtx, "wss://"+hostname+"/echo", &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}})
	dialCancel()
	if err != nil {
		cancel()
		tr.CloseIdleConnections()
		return err
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	c.conn = conn
	c.cancel = cancel
	c.httpTransport = tr
	conn.SetReadLimit(4096)
	resumed := resp != nil && resp.TLS != nil && resp.TLS.DidResume
	c.emit("connected", map[string]any{"connect_ms": float64(time.Since(started)) / float64(time.Millisecond), "transport": "wss", "tls_resumed": resumed})
	c.wg.Add(2)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(time.Duration(intervalMS) * time.Millisecond)
		defer ticker.Stop()
		var seq uint64
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				seq++
				writeCtx, done := context.WithTimeout(ctx, 5*time.Second)
				err := wsjson.Write(writeCtx, conn, protocol.Frame{Seq: seq, SentNS: time.Since(started).Nanoseconds()})
				if err == nil && seq%uint64(max(1, 1000/intervalMS)) == 0 {
					err = wsjson.Write(writeCtx, conn, protocol.Frame{Transit: true, Seq: seq, SentNS: time.Since(started).Nanoseconds()})
				}
				done()
				if err != nil {
					cancel()
					conn.CloseNow()
					return
				}
			}
		}
	}()
	go func() {
		defer c.wg.Done()
		last := started
		for {
			var f protocol.Frame
			err := wsjson.Read(ctx, conn, &f)
			if err != nil {
				c.emit("disconnected", map[string]any{"reason": err.Error()})
				cancel()
				conn.CloseNow()
				return
			}
			if f.Transit {
				emitTransit(c.emit, f, float64(time.Since(started).Nanoseconds()-f.SentNS)/1e6)
				continue
			}
			now := time.Now()
			c.emit("echo", map[string]any{"seq": f.Seq, "rtt_ms": float64(time.Since(started).Nanoseconds()-f.SentNS) / 1e6, "gap_ms": float64(now.Sub(last)) / float64(time.Millisecond), "connection_id": f.ConnectionID})
			last = now
		}
	}()
	return nil
}

func (c *WebSocketClient) Stop() {
	c.op.Lock()
	defer c.op.Unlock()
	if c.conn == nil {
		return
	}
	c.cancel()
	c.conn.CloseNow()
	c.wg.Wait()
	c.httpTransport.CloseIdleConnections()
	c.conn = nil
	c.cancel = nil
	c.httpTransport = nil
}
