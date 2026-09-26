package mobile

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"net"
	"quiclab/internal/awg"
	"quiclab/internal/gateway"
	"time"
)

// ValidateAWGConfig returns only public metadata; keys stay in the encrypted profile.
func ValidateAWGConfig(raw string) (string, error) {
	c, e := awg.Parse(raw)
	if e != nil {
		return "", e
	}
	return c.Metadata(), nil
}

// flowDialer is the boundary between the Android flow router and protocol cores.
// Future cores (including proxy engines) need not implement the QUIC wire protocol.
type flowDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}
type tcpFlow struct{ net.Conn }

func (c tcpFlow) CloseWrite() error {
	if v, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return v.CloseWrite()
	}
	return c.Close()
}
func (g *Gateway) dialStream(ctx context.Context, kind, target string) (gateway.Stream, error) {
	g.mu.Lock()
	d := g.direct
	if g.awg != nil {
		d = g.awg
	}
	g.mu.Unlock()
	if d == nil {
		return gateway.Open(ctx, g.open, kind, target)
	}
	if kind == "exit-ip" {
		target = "api.ipify.org:443"
	}
	c, e := d.DialContext(ctx, "tcp4", target)
	if e != nil {
		return nil, e
	}
	return tcpFlow{c}, nil
}
func (g *Gateway) datagramBackend() flowDialer {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.direct != nil {
		return g.direct
	}
	if g.awg != nil {
		return g.awg
	}
	if g.mux != nil && g.udpStream {
		return httpsUDP{g}
	}
	if g.datagrams != nil {
		return quicUDP{g}
	}
	return nil
}

// RTT is an ICMP round trip to the endpoint through AWG.
// Probes also drive roaming when PersistentKeepalive is zero.
func (g *Gateway) awgHeartbeat(ctx context.Context, d flowDialer, endpoint string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var seq uint16
	for {
		if ctx.Err() != nil {
			return
		}
		seq++
		started := time.Now()
		probe, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		e := awgPingProbe(probe, d, endpoint, seq)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if e == nil {
			g.emit("echo", map[string]any{"rtt_ms": float64(time.Since(started)) / float64(time.Millisecond), "connection_id": "awg", "probe": "icmp"})
		} else {
			g.emit("probe_unavailable", map[string]any{"detail": "Endpoint ICMP: no reply through AWG"})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func awgPingProbe(ctx context.Context, d flowDialer, target string, seq uint16) error {
	c, e := d.DialContext(ctx, "ping4", target)
	if e != nil {
		return e
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	c.SetDeadline(deadline)
	message := icmp.Message{Type: ipv4.ICMPTypeEcho, Code: 0, Body: &icmp.Echo{ID: 1, Seq: int(seq), Data: []byte("quic-lab-rtt")}}
	packet, e := message.Marshal(nil)
	if e != nil {
		return e
	}
	if _, e = c.Write(packet); e != nil {
		return e
	}
	b := make([]byte, 256)
	n, e := c.Read(b)
	if e != nil {
		return e
	}
	response, e := icmp.ParseMessage(1, b[:n])
	if e != nil {
		return e
	}
	echo, ok := response.Body.(*icmp.Echo)
	if !ok || response.Type != ipv4.ICMPTypeEchoReply || echo.Seq != int(seq) || string(echo.Data) != "quic-lab-rtt" {
		return errors.New("invalid ICMP echo reply")
	}
	return nil
}

// This is end-to-end RTT to the upstream Endpoint. It never feeds the local
// heartbeat, migration decisions, jitter window or the local-RTT graph.
func (g *Gateway) transitHeartbeat(ctx context.Context, awgDialer flowDialer, endpoint string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var seq uint16
	for {
		if ctx.Err() != nil {
			return
		}
		seq++
		started := time.Now()
		probe, cancel := context.WithTimeout(ctx, 2*time.Second)
		var e error
		if awgDialer != nil {
			if g.cfg.TransitUDP {
				e = transitUDPProbe(probe, awgDialer, endpoint)
			} else {
				e = awgPingProbe(probe, awgDialer, endpoint, seq)
			}
		} else {
			var stream gateway.Stream
			stream, e = gateway.Open(probe, g.open, "transit-probe", "")
			if stream != nil {
				stream.Close()
			}
		}
		cancel()
		if ctx.Err() != nil {
			return
		}
		if e == nil {
			g.emit("transit_echo", map[string]any{"rtt_ms": float64(time.Since(started)) / float64(time.Millisecond)})
		} else {
			g.emit("transit_probe_failed", map[string]any{})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type quicUDP struct{ g *Gateway }

func (d quicUDP) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "udp4" {
		return nil, errors.New("UDP only")
	}
	d.g.mu.Lock()
	m, q := d.g.datagrams, d.g.q
	d.g.mu.Unlock()
	if m == nil || q == nil {
		return nil, errors.New("QUIC disconnected")
	}
	q.op.Lock()
	c := q.conn
	q.op.Unlock()
	if c == nil {
		return nil, errors.New("QUIC disconnected")
	}
	return m.DialUDP(ctx, c, address)
}

type httpsUDP struct{ g *Gateway }

func (d httpsUDP) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "udp4" {
		return nil, errors.New("UDP only")
	}
	st, e := gateway.Open(ctx, d.g.open, "udp-stream-v1", address)
	if e != nil {
		return nil, e
	}
	return &gateway.PacketStream{Stream: st}, nil
}

// Echo-only AWG transit service accepts an opaque nonce, never a destination.
func transitUDPProbe(ctx context.Context, d flowDialer, target string) error {
	c, e := d.DialContext(ctx, "udp4", target)
	if e != nil {
		return e
	}
	defer c.Close()
	deadline, _ := ctx.Deadline()
	c.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	b := make([]byte, 24)
	copy(b, "QLPROBE1")
	if _, e = rand.Read(b[8:]); e != nil {
		return e
	}
	if _, e = c.Write(b); e != nil {
		return e
	}
	response := make([]byte, 32)
	n, e := c.Read(response)
	if e != nil {
		return e
	}
	if !bytes.Equal(b, response[:n]) {
		return errors.New("invalid transit echo")
	}
	return nil
}
