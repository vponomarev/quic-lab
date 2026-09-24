// Package gateway implements the authenticated proxy, independent of the echo protocol.
package gateway

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/xtaci/smux"
)

const ALPN = "quic-lab-gateway/1"
const MaxFlows = 128

// Stream preserves TCP half-close for both QUIC and smux.
type Stream interface {
	io.ReadWriteCloser
	SetDeadline(time.Time) error
	CloseWrite() error
}
type QStream struct{ *quic.Stream }

func (s QStream) CloseWrite() error { return s.Stream.Close() }
func (s QStream) Close() error {
	s.CancelRead(0)
	s.SetWriteDeadline(time.Now())
	return s.Stream.Close()
}

// Application framing provides FIN independently of smux stream cleanup.
type MStream struct {
	raw         *smux.Stream
	pending     []byte
	eof         bool
	writeMu     sync.Mutex
	writeClosed bool
}

func NewMStream(s *smux.Stream) *MStream { return &MStream{raw: s} }
func (s *MStream) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if len(s.pending) == 0 {
		if s.eof {
			return 0, io.EOF
		}
		p, e := ReadPacket(s.raw, 32768)
		if e != nil {
			return 0, e
		}
		if len(p) == 0 {
			s.eof = true
			return 0, io.EOF
		}
		s.pending = p
	}
	n := copy(b, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}
func (s *MStream) Write(b []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeClosed {
		return 0, io.ErrClosedPipe
	}
	total := 0
	for len(b) > 0 {
		n := len(b)
		if n > 32768 {
			n = 32768
		}
		if e := WritePacket(s.raw, b[:n]); e != nil {
			return total, e
		}
		total += n
		b = b[n:]
	}
	return total, nil
}
func (s *MStream) CloseWrite() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeClosed {
		return nil
	}
	s.writeClosed = true
	return WritePacket(s.raw, nil)
}
func (s *MStream) Close() error                  { return s.raw.Close() }
func (s *MStream) SetDeadline(t time.Time) error { return s.raw.SetDeadline(t) }

func MuxConfig() *smux.Config {
	c := smux.DefaultConfig()
	c.Version = 2
	c.MaxReceiveBuffer = 4 << 20
	c.MaxStreamBuffer = 128 << 10
	c.KeepAliveInterval = 2 * time.Second
	c.KeepAliveTimeout = 15 * time.Second
	return c
}

type Request struct {
	Network string `json:"network"`
	Address string `json:"address,omitempty"`
}
type Reply struct {
	Error   string `json:"error,omitempty"`
	Session string `json:"session,omitempty"`
}

func WriteJSON(w io.Writer, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return WritePacket(w, b)
}
func ReadJSON(r io.Reader, v any) error {
	b, e := ReadPacket(r, 4096)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func WritePacket(w io.Writer, b []byte) error {
	if len(b) > 65535 {
		return errors.New("frame too large")
	}
	h := []byte{0, 0}
	binary.BigEndian.PutUint16(h, uint16(len(b)))
	if _, e := w.Write(h); e != nil {
		return e
	}
	_, e := w.Write(b)
	return e
}
func ReadPacket(r io.Reader, max int) ([]byte, error) {
	var h [2]byte
	if _, e := io.ReadFull(r, h[:]); e != nil {
		return nil, e
	}
	n := int(binary.BigEndian.Uint16(h[:]))
	if n > max {
		return nil, errors.New("frame limit")
	}
	b := make([]byte, n)
	_, e := io.ReadFull(r, b)
	return b, e
}
func Open(ctx context.Context, open func(context.Context) (Stream, error), network, address string) (Stream, error) {
	s, e := open(ctx)
	if e != nil {
		return nil, e
	}
	s.SetDeadline(time.Now().Add(10 * time.Second))
	if e = WriteJSON(s, Request{network, address}); e == nil {
		var reply Reply
		e = ReadJSON(s, &reply)
		if e == nil && reply.Error != "" {
			e = errors.New(reply.Error)
		}
	}
	if e != nil {
		s.Close()
		return nil, e
	}
	s.SetDeadline(time.Time{})
	return s, nil
}

// Relay bounds memory and preserves a request-side FIN while a response is pending.
func Relay(ctx context.Context, a Stream, b net.Conn) (int64, int64) {
	stop := context.AfterFunc(ctx, func() { a.Close(); b.Close() })
	defer stop()
	defer a.Close()
	defer b.Close()
	ch := make(chan int64, 1)
	go func() {
		n, e := io.Copy(b, struct{ io.Reader }{a})
		if e != nil {
			b.Close()
			a.Close()
		} else if c, ok := b.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
		ch <- n
	}()
	down, e := io.Copy(a, b)
	if e != nil {
		a.Close()
		b.Close()
	} else {
		a.CloseWrite()
	}
	up := <-ch
	return up, down
}
