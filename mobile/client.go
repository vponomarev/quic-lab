// Package mobile exposes a gomobile-compatible API shared with the CLI smoke test.
package mobile

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"
	"quiclab/internal/gateway"
	"quiclab/internal/protocol"
)

// SocketBinder runs before any packet is sent. The fd is borrowed, never owned.
// Android must duplicate it with ParcelFileDescriptor.fromFd and close only the duplicate.
type SocketBinder interface{ Bind(fd int64) error }

// EventSink may be called from worker goroutines; it must not reenter Client methods.
type EventSink interface{ OnEvent(eventJSON string) }

type transport struct {
	q   *quic.Transport
	udp *net.UDPConn
}

func (t transport) close() { t.q.Close(); t.udp.Close() }

type preparedPath struct {
	key       string
	transport transport
	path      *quic.Path
	checked   time.Time
}

type Client struct {
	gatewayTLS *tls.Config

	prepared   *preparedPath
	parked     *preparedPath // retain another candidate while both networks recover
	retirement sync.WaitGroup

	op           sync.Mutex
	events       sync.Mutex
	sink         EventSink
	conn         *quic.Conn
	activePath   *quic.Path
	paths        []transport
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	newTransport func(net.IP, SocketBinder, func(net.Addr, error)) (transport, error)
}

func NewClient(sink EventSink) *Client { return &Client{sink: sink, newTransport: openTransport} }

func (c *Client) emit(kind string, fields map[string]any) {
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

func openTransport(ip net.IP, binder SocketBinder, onUnavailable func(net.Addr, error)) (transport, error) {
	family, local := "udp4", "0.0.0.0:0"
	if ip.To4() == nil {
		family, local = "udp6", "[::]:0"
	}
	lc := net.ListenConfig{}
	if binder != nil {
		lc.Control = func(_, _ string, raw syscall.RawConn) error {
			var bindErr error
			err := raw.Control(func(fd uintptr) { bindErr = binder.Bind(int64(fd)) })
			if err != nil {
				return err
			}
			return bindErr
		}
	}
	pc, err := lc.ListenPacket(context.Background(), family, local)
	if err != nil {
		return transport{}, err
	}
	udp := pc.(*net.UDPConn)
	return transport{q: &quic.Transport{Conn: &pathSocket{PacketConn: udp, onUnavailable: onUnavailable}}, udp: udp}, nil
}

func (c *Client) pathUnavailable(local net.Addr, err error) {
	c.emit("path_unavailable", map[string]any{"local": local.String(), "error": err.Error()})
}

func clientTLS(serverName, fingerprint string) (*tls.Config, error) {
	cfg := &tls.Config{ServerName: serverName, NextProtos: []string{protocol.ALPN}, MinVersion: tls.VersionTLS13}
	if fingerprint == "" {
		return cfg, nil
	}
	pin, err := hex.DecodeString(fingerprint)
	if err != nil || len(pin) != sha256.Size {
		return nil, errors.New("certificate SHA-256 must contain 64 hexadecimal characters")
	}
	// Explicit certificate pin replaces PKI validation only for the lab's self-signed certificate.
	cfg.InsecureSkipVerify = true
	cfg.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return errors.New("no peer certificate")
		}
		cert := state.PeerCertificates[0]
		sum := sha256.Sum256(cert.Raw)
		if subtle.ConstantTimeCompare(pin, sum[:]) != 1 {
			return errors.New("certificate fingerprint mismatch")
		}
		if time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
			return errors.New("certificate expired or not yet valid")
		}
		return nil
	}
	return cfg, nil
}

// Start opens exactly one connection and one stream. It never automatically reconnects.
// endpoint must be an IP:port (IPv6 in brackets). DNS resolution belongs to Android Network.
func (c *Client) Start(endpoint, serverName, fingerprint string, intervalMS int, binder SocketBinder) error {
	c.op.Lock()
	defer c.op.Unlock()
	if c.conn != nil {
		return errors.New("stop the current experiment first")
	}
	if intervalMS < 10 || intervalMS > 5000 {
		return errors.New("prototype interval must be 10..5000 ms")
	}
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil || net.ParseIP(host) == nil {
		return errors.New("endpoint must be numeric IP:port")
	}
	addr, err := net.ResolveUDPAddr("udp", endpoint)
	if err != nil {
		return err
	}
	cfg, err := clientTLS(serverName, fingerprint)
	if err != nil {
		return err
	}
	if c.gatewayTLS != nil {
		cfg = c.gatewayTLS.Clone()
	}
	t, err := c.newTransport(addr.IP, binder, c.pathUnavailable)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	conn, err := t.q.Dial(ctx, addr, cfg, &quic.Config{EnableDatagrams: c.gatewayTLS != nil, MaxIdleTimeout: 90 * time.Second, KeepAlivePeriod: 2 * time.Second})
	if err != nil {
		cancel()
		t.close()
		return err
	}
	stream, err := conn.OpenStreamSync(ctx)
	cancel()
	if err != nil {
		conn.CloseWithError(1, "stream failed")
		t.close()
		return err
	}
	if c.gatewayTLS != nil {
		stream.SetDeadline(time.Now().Add(10 * time.Second))
		err = gateway.WriteJSON(stream, gateway.Request{Network: "echo"})
		if err == nil {
			var reply gateway.Reply
			err = gateway.ReadJSON(stream, &reply)
			if err == nil && reply.Error != "" {
				err = errors.New(reply.Error)
			}
		}
		if err != nil {
			conn.CloseWithError(1, "gateway echo failed")
			t.close()
			return err
		}
		stream.SetDeadline(time.Time{})
	}
	ctx, c.cancel = context.WithCancel(context.Background())
	c.conn = conn
	c.paths = []transport{t}
	c.emit("connected", map[string]any{"local": t.udp.LocalAddr().String(), "remote": endpoint, "stream_id": stream.StreamID()})
	start := time.Now()
	c.wg.Add(2)
	go func() {
		defer c.wg.Done()
		enc := json.NewEncoder(stream)
		ticker := time.NewTicker(time.Duration(intervalMS) * time.Millisecond)
		defer ticker.Stop()
		var seq uint64
		for {
			select {
			case <-ctx.Done():
				return
			case <-conn.Context().Done():
				return
			case <-ticker.C:
				seq++
				stream.SetWriteDeadline(time.Now().Add(90 * time.Second))
				if err := enc.Encode(protocol.Frame{Seq: seq, SentNS: time.Since(start).Nanoseconds()}); err != nil {
					c.emit("send_failed", map[string]any{"error": err.Error()})
					conn.CloseWithError(1, "send failed")
					return
				}
				if c.gatewayTLS == nil && seq%uint64(max(1, 1000/intervalMS)) == 0 {
					enc.Encode(protocol.Frame{Transit: true, Seq: seq, SentNS: time.Since(start).Nanoseconds()})
				}
			}
		}
	}()
	go func() {
		defer c.wg.Done()
		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 1024), 4096)
		last := start
		for scanner.Scan() {
			var frame protocol.Frame
			if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
				conn.CloseWithError(1, "invalid echo")
				break
			}
			if frame.Transit {
				emitTransit(c.emit, frame, float64(time.Since(start).Nanoseconds()-frame.SentNS)/1e6)
				continue
			}
			now := time.Now()
			c.emit("echo", map[string]any{"seq": frame.Seq, "rtt_ms": float64(time.Since(start).Nanoseconds()-frame.SentNS) / 1e6,
				"gap_ms": float64(now.Sub(last).Nanoseconds()) / 1e6, "connection_id": frame.ConnectionID, "stream_id": frame.StreamID, "peer": frame.Peer})
			last = now
		}
		c.emit("disconnected", map[string]any{"reason": fmt.Sprint(scanner.Err()), "auto_reconnect": false})
		conn.CloseWithError(0, "reader finished")
	}()
	return nil
}

// PreparePath validates a standby without moving application traffic. The short
// timeout bounds how long the Android control worker waits for a bad reserve.
func (c *Client) PreparePath(key string, binder SocketBinder) error {
	c.op.Lock()
	defer c.op.Unlock()
	return c.prepare(key, binder, 250*time.Millisecond)
}

func (c *Client) prepare(key string, binder SocketBinder, timeout time.Duration) error {
	if c.conn == nil {
		return errors.New("not connected")
	}
	if err := c.conn.Context().Err(); err != nil {
		return fmt.Errorf("connection closed: %w", context.Cause(c.conn.Context()))
	}
	if c.prepared != nil && c.prepared.key != key {
		if c.parked != nil && c.parked.key == key {
			c.prepared, c.parked = c.parked, c.prepared
		} else {
			c.discardParked()
			c.parked, c.prepared = c.prepared, nil
		}
	}
	if c.prepared == nil && c.parked != nil && c.parked.key == key {
		c.prepared, c.parked = c.parked, nil
	}
	if c.prepared == nil {
		tr, err := c.newTransport(c.conn.RemoteAddr().(*net.UDPAddr).IP, binder, c.pathUnavailable)
		if err != nil {
			return err
		}
		path, err := c.conn.AddPath(tr.q)
		if err != nil {
			tr.close()
			return err
		}
		c.prepared = &preparedPath{key: key, transport: tr, path: path}
	}
	ctx, cancel := context.WithTimeout(c.conn.Context(), timeout)
	defer cancel()
	started := time.Now()
	if err := c.prepared.path.Probe(ctx); err != nil {
		// A timeout says nothing about the lifetime of this Android Network.
		// Keep its socket and CID so retries don't exhaust IDs while the active
		// path is down and replacement IDs cannot arrive from the peer.
		c.prepared.checked = time.Time{}
		return err
	}
	c.prepared.checked = time.Now()
	c.emit("standby_ready", map[string]any{"key": key, "probe_ms": float64(time.Since(started)) / float64(time.Millisecond)})
	return nil
}

func (c *Client) discardPrepared() {
	if c.prepared == nil {
		return
	}
	c.prepared.path.Close()
	c.retire(c.prepared.transport)
	c.prepared = nil
}

func (c *Client) discardParked() {
	if c.parked == nil {
		return
	}
	c.parked.path.Close()
	c.retire(c.parked.transport)
	c.parked = nil
}

// InvalidatePath prevents using a standby whose Android Network was lost.
func (c *Client) InvalidatePath(key string) {
	c.op.Lock()
	defer c.op.Unlock()
	if c.prepared != nil && c.prepared.key == key {
		c.discardPrepared()
	}
	if c.parked != nil && c.parked.key == key {
		c.discardParked()
	}
}

func (c *Client) retire(tr transport) {
	conn := c.conn
	c.retirement.Add(1)
	go func() {
		defer c.retirement.Done()
		// Switch is asynchronous. Wait until the connection loop has selected the
		// new transport before detaching this one. Keep late packets for a short grace.
		timer := time.NewTimer(150 * time.Millisecond)
		select {
		case <-timer.C:
		case <-conn.Context().Done():
			timer.Stop()
		}
		for {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			err := conn.RetireTransport(ctx, tr.q)
			cancel()
			if err == nil {
				tr.close()
				return
			}
			select {
			case <-conn.Context().Done():
				tr.close()
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
}

// Migrate preserves the CLI API; Android uses a key identifying its Network.
func (c *Client) Migrate(binder SocketBinder) error { return c.MigrateTo("", binder) }

func (c *Client) MigrateTo(key string, binder SocketBinder) error {
	c.op.Lock()
	defer c.op.Unlock()
	if c.conn == nil {
		return errors.New("not connected")
	}
	if err := c.conn.Context().Err(); err != nil {
		return fmt.Errorf("connection closed: %w", context.Cause(c.conn.Context()))
	}
	warm := c.prepared != nil && c.prepared.key == key && time.Since(c.prepared.checked) < 3*time.Second
	if !warm {
		c.emit("path_probing", map[string]any{"key": key})
		if err := c.prepare(key, binder, 700*time.Millisecond); err != nil {
			return err
		}
	}
	next := c.prepared
	if err := next.path.Switch(); err != nil {
		c.discardPrepared()
		return err
	}
	old := c.paths[0]
	if c.activePath != nil {
		c.activePath.Close()
	}
	c.paths = []transport{next.transport}
	c.activePath = next.path
	c.prepared = nil
	c.retire(old)
	c.emit("path_switched", map[string]any{"local": next.transport.udp.LocalAddr().String(), "prepared": warm})
	return nil
}

// LocalAddress identifies the current socket for filtering stale path-error events.
func (c *Client) LocalAddress() string {
	c.op.Lock()
	defer c.op.Unlock()
	if len(c.paths) == 0 {
		return ""
	}
	return c.paths[len(c.paths)-1].udp.LocalAddr().String()
}

func (c *Client) Stop() {
	c.op.Lock()
	defer c.op.Unlock()
	if c.conn == nil {
		return
	}
	c.cancel()
	c.discardPrepared()
	c.discardParked()
	c.conn.CloseWithError(0, "experiment stopped")
	c.wg.Wait()
	c.retirement.Wait()
	for _, t := range c.paths {
		t.close()
	}
	c.paths = nil
	c.conn = nil
	c.activePath = nil
}
