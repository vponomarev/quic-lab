package gateway

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// PacketStream preserves UDP boundaries over an ordered, reliable tunnel stream.
// Each flow has independent read/write loops, but TCP HoL still spans the session.
type PacketStream struct {
	Stream
	readMu, writeMu sync.Mutex
}

func (p *PacketStream) Read(b []byte) (int, error) {
	p.readMu.Lock()
	defer p.readMu.Unlock()
	packet, e := ReadPacket(p.Stream, maxUDP)
	if e != nil {
		return 0, e
	}
	return copy(b, packet), nil
}
func (p *PacketStream) Write(b []byte) (int, error) {
	if len(b) > maxUDP {
		return 0, errors.New("UDP payload too large")
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if e := WritePacket(p.Stream, b); e != nil {
		return 0, e
	}
	return len(b), nil
}
func (p *PacketStream) LocalAddr() net.Addr  { return &net.UDPAddr{} }
func (p *PacketStream) RemoteAddr() net.Addr { return &net.UDPAddr{} }
func (p *PacketStream) SetReadDeadline(t time.Time) error {
	if s, ok := p.Stream.(interface{ SetReadDeadline(time.Time) error }); ok {
		return s.SetReadDeadline(t)
	}
	return p.Stream.SetDeadline(t)
}
func (p *PacketStream) SetWriteDeadline(t time.Time) error {
	if s, ok := p.Stream.(interface{ SetWriteDeadline(time.Time) error }); ok {
		return s.SetWriteDeadline(t)
	}
	return p.Stream.SetDeadline(t)
}
func (s *MStream) SetReadDeadline(t time.Time) error  { return s.raw.SetReadDeadline(t) }
func (s *MStream) SetWriteDeadline(t time.Time) error { return s.raw.SetWriteDeadline(t) }

func (s *Server) serveUDPStream(ctx context.Context, st Stream, req Request, id string, count func(int, int)) {
	if _, ok := st.(*MStream); !ok {
		WriteJSON(st, Reply{Error: "UDP stream requires HTTPS transport"})
		return
	}
	if !s.Permitted(req.Address) {
		WriteJSON(st, Reply{Error: "destination denied"})
		return
	}
	dial := s.DialContext
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	dctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	out, e := dial(dctx, "udp4", req.Address)
	cancel()
	if e != nil {
		WriteJSON(st, Reply{Error: "destination unavailable"})
		return
	}
	defer out.Close()
	if WriteJSON(st, Reply{Session: id}) != nil {
		return
	}
	packets := &PacketStream{Stream: st}
	flowCtx, stop := context.WithCancel(ctx)
	defer stop()
	shutdown := context.AfterFunc(flowCtx, func() { st.Close(); out.Close() })
	defer shutdown()
	touch := func() {
		deadline := time.Now().Add(60 * time.Second)
		st.SetDeadline(deadline)
		out.SetDeadline(deadline)
	}
	touch()
	s.Log.Info("flow_open", "session", id, "network", "udp", "transport", "https-udp-stream", "destination", req.Address)
	var up, down atomic.Int64
	defer func() {
		s.Log.Info("flow_bytes", "session", id, "network", "udp", "transport", "https-udp-stream", "destination", req.Address, "up", up.Load(), "down", down.Load())
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer stop()
		b := make([]byte, maxUDP)
		for {
			n, e := out.Read(b)
			if e != nil {
				return
			}
			if _, e = packets.Write(b[:n]); e != nil {
				return
			}
			down.Add(int64(n))
			if count != nil {
				count(0, n)
			}
			touch()
		}
	}()
	defer func() { stop(); out.Close(); st.Close(); <-done }()
	b := make([]byte, maxUDP)
	for {
		n, e := packets.Read(b)
		if e != nil {
			return
		}
		if _, e = out.Write(b[:n]); e != nil {
			return
		}
		up.Add(int64(n))
		if count != nil {
			count(n, 0)
		}
		touch()
	}
}

var _ net.Conn = (*PacketStream)(nil)
