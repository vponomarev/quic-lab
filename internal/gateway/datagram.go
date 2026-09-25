package gateway

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
)

// A reliable control stream authorizes each flow. Its stream ID is the datagram
// context ID, never reused within the connection. Payloads are never retransmitted.
const datagramChunk = 1000
const maxUDP = 65507
const fragmentLifetime = 500 * time.Millisecond

type datagramTransport interface {
	Context() context.Context
	SendDatagram([]byte) error
	ReceiveDatagram(context.Context) ([]byte, error)
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
}
type queuedDatagram struct {
	flow    *DatagramFlow
	payload []byte
	at      time.Time
}
type DatagramMux struct {
	conn  datagramTransport
	mu    sync.Mutex
	flows map[uint64]*DatagramFlow
	send  chan queuedDatagram
	Drops atomic.Int64
}

func NewDatagramMux(c *quic.Conn) *DatagramMux { return newDatagramMux(c) }
func newDatagramMux(c datagramTransport) *DatagramMux {
	m := &DatagramMux{conn: c, flows: make(map[uint64]*DatagramFlow), send: make(chan queuedDatagram, 32)}
	go m.receive()
	go m.sender()
	return m
}
func (m *DatagramMux) Register(id uint64) (*DatagramFlow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn.Context().Err() != nil {
		return nil, net.ErrClosed
	}
	if len(m.flows) >= MaxFlows {
		return nil, errors.New("UDP flow limit")
	}
	if m.flows[id] != nil {
		return nil, errors.New("duplicate UDP context")
	}
	f := &DatagramFlow{mux: m, id: id, recv: make(chan []byte, 32), done: make(chan struct{}), changed: make(chan struct{}), parts: make(map[uint32]*assembly)}
	m.flows[id] = f
	return f, nil
}
func (m *DatagramMux) receive() {
	for {
		b, e := m.conn.ReceiveDatagram(m.conn.Context())
		if e != nil {
			return
		}
		if len(b) < 16 || len(b) > 16+datagramChunk {
			m.Drops.Add(1)
			continue
		}
		m.mu.Lock()
		f := m.flows[binary.BigEndian.Uint64(b)]
		m.mu.Unlock()
		if f == nil {
			m.Drops.Add(1)
			continue
		}
		f.accept(b)
	}
}
func (m *DatagramMux) sender() {
	for {
		select {
		case <-m.conn.Context().Done():
			return
		case q := <-m.send:
			select {
			case <-q.flow.done:
				continue
			default:
			}
			if time.Since(q.at) > 200*time.Millisecond {
				m.Drops.Add(1)
				continue
			}
			if m.conn.SendDatagram(q.payload) != nil {
				m.Drops.Add(1)
			}
		}
	}
}

type assembly struct {
	at          time.Time
	pieces      [][]byte
	count, size int
}
type DatagramFlow struct {
	mux                         *DatagramMux
	id                          uint64
	seq                         atomic.Uint32
	recv                        chan []byte
	done                        chan struct{}
	once                        sync.Once
	control                     Stream
	mu                          sync.Mutex
	readDeadline, writeDeadline time.Time
	changed                     chan struct{}
	parts                       map[uint32]*assembly // accessed only by the connection receive goroutine
}

func (f *DatagramFlow) accept(b []byte) {
	select {
	case <-f.done:
		return
	default:
	}
	seq := binary.BigEndian.Uint32(b[8:])
	index := int(binary.BigEndian.Uint16(b[12:]))
	total := int(binary.BigEndian.Uint16(b[14:]))
	payload := b[16:]
	if total < 1 || total > 66 || index >= total || (index < total-1 && len(payload) != datagramChunk) || (total > 1 && index == total-1 && len(payload) == 0) {
		f.mux.Drops.Add(1)
		return
	}
	if total == 1 {
		f.deliver(payload)
		return
	}
	now := time.Now()
	for id, a := range f.parts {
		if now.Sub(a.at) > fragmentLifetime {
			delete(f.parts, id)
			f.mux.Drops.Add(1)
		}
	}
	a := f.parts[seq]
	if a == nil {
		if len(f.parts) >= 8 {
			f.mux.Drops.Add(1)
			return
		}
		a = &assembly{at: now, pieces: make([][]byte, total)}
		f.parts[seq] = a
	}
	if len(a.pieces) != total {
		f.mux.Drops.Add(1)
		return
	}
	if a.pieces[index] != nil {
		return
	}
	a.pieces[index] = append([]byte{}, payload...)
	a.count++
	a.size += len(payload)
	if a.size > maxUDP {
		delete(f.parts, seq)
		f.mux.Drops.Add(1)
		return
	}
	if a.count == total {
		packet := make([]byte, 0, a.size)
		for _, p := range a.pieces {
			packet = append(packet, p...)
		}
		delete(f.parts, seq)
		f.deliver(packet)
	}
}
func (f *DatagramFlow) deliver(b []byte) {
	select {
	case f.recv <- b:
	default:
		f.mux.Drops.Add(1)
	}
}
func (f *DatagramFlow) Read(b []byte) (int, error) {
	for {
		f.mu.Lock()
		deadline, changed := f.readDeadline, f.changed
		f.mu.Unlock()
		var timer *time.Timer
		var timeout <-chan time.Time
		if !deadline.IsZero() {
			if !time.Now().Before(deadline) {
				return 0, os.ErrDeadlineExceeded
			}
			timer = time.NewTimer(time.Until(deadline))
			timeout = timer.C
		}
		select {
		case <-f.done:
			if timer != nil {
				timer.Stop()
			}
			return 0, net.ErrClosed
		case <-f.mux.conn.Context().Done():
			if timer != nil {
				timer.Stop()
			}
			return 0, net.ErrClosed
		case <-changed:
			if timer != nil {
				timer.Stop()
			}
			continue
		case <-timeout:
			return 0, os.ErrDeadlineExceeded
		case p := <-f.recv:
			if timer != nil {
				timer.Stop()
			}
			return copy(b, p), nil
		}
	}
}
func (f *DatagramFlow) Write(b []byte) (int, error) {
	select {
	case <-f.done:
		return 0, net.ErrClosed
	case <-f.mux.conn.Context().Done():
		return 0, net.ErrClosed
	default:
	}
	f.mu.Lock()
	deadline := f.writeDeadline
	f.mu.Unlock()
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		return 0, os.ErrDeadlineExceeded
	}
	if len(b) > maxUDP {
		return 0, errors.New("UDP payload too large")
	}
	seq := f.seq.Add(1)
	total := max(1, (len(b)+datagramChunk-1)/datagramChunk)
	now := time.Now()
	for i := 0; i < total; i++ {
		start := i * datagramChunk
		end := min(len(b), start+datagramChunk)
		p := make([]byte, 16+end-start)
		binary.BigEndian.PutUint64(p, f.id)
		binary.BigEndian.PutUint32(p[8:], seq)
		binary.BigEndian.PutUint16(p[12:], uint16(i))
		binary.BigEndian.PutUint16(p[14:], uint16(total))
		copy(p[16:], b[start:end])
		select {
		case f.mux.send <- queuedDatagram{f, p, now}:
		default:
			f.mux.Drops.Add(1)
			return len(b), nil
		}
	}
	return len(b), nil
}
func (f *DatagramFlow) Close() error {
	f.once.Do(func() {
		close(f.done)
		f.mux.mu.Lock()
		delete(f.mux.flows, f.id)
		f.mux.mu.Unlock()
		if f.control != nil {
			f.control.Close()
		}
	})
	return nil
}
func (f *DatagramFlow) LocalAddr() net.Addr  { return f.mux.conn.LocalAddr() }
func (f *DatagramFlow) RemoteAddr() net.Addr { return f.mux.conn.RemoteAddr() }
func (f *DatagramFlow) SetDeadline(t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readDeadline = t
	f.writeDeadline = t
	close(f.changed)
	f.changed = make(chan struct{})
	return nil
}
func (f *DatagramFlow) SetReadDeadline(t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readDeadline = t
	close(f.changed)
	f.changed = make(chan struct{})
	return nil
}
func (f *DatagramFlow) SetWriteDeadline(t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeDeadline = t
	return nil
}

// DialUDP keeps the authorization stream alive for exactly the flow lifetime.
func (m *DatagramMux) DialUDP(ctx context.Context, c *quic.Conn, address string) (net.Conn, error) {
	st, e := c.OpenStreamSync(ctx)
	if e != nil {
		return nil, e
	}
	control := QStream{st}
	f, e := m.Register(uint64(st.StreamID()))
	if e != nil {
		control.Close()
		return nil, e
	}
	f.control = control
	stop := context.AfterFunc(ctx, func() { control.Close() })
	defer stop()
	st.SetDeadline(time.Now().Add(10 * time.Second))
	if e = WriteJSON(st, Request{Network: "udp-datagram-v1", Address: address}); e == nil {
		var reply Reply
		e = ReadJSON(st, &reply)
		if e == nil && reply.Error != "" {
			e = errors.New(reply.Error)
		}
	}
	if e != nil {
		f.Close()
		return nil, e
	}
	st.SetDeadline(time.Time{})
	go func() { var b [1]byte; st.Read(b[:]); f.Close() }()
	return f, nil
}

func (s *Server) serveUDP(ctx context.Context, st Stream, req Request, id string, m *DatagramMux, count func(int, int)) {
	q, ok := st.(QStream)
	if !ok || m == nil {
		WriteJSON(st, Reply{Error: "QUIC DATAGRAM unavailable"})
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
	dialCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, e := dial(dialCtx, "udp4", req.Address)
	if e != nil {
		WriteJSON(st, Reply{Error: "destination unavailable"})
		return
	}
	defer out.Close()
	f, e := m.Register(uint64(q.StreamID()))
	if e != nil {
		WriteJSON(st, Reply{Error: e.Error()})
		return
	}
	defer f.Close()
	if WriteJSON(st, Reply{Session: id}) != nil {
		return
	}
	st.SetDeadline(time.Time{})
	flowCtx, stop := context.WithCancel(ctx)
	defer stop()
	closeOnStop := context.AfterFunc(flowCtx, func() { out.Close(); f.Close(); st.Close() })
	defer closeOnStop()
	controlDone := make(chan struct{})
	go func() { defer close(controlDone); var b [1]byte; st.Read(b[:]); stop() }()
	defer func() { stop(); st.Close(); <-controlDone }()
	s.Log.Info("flow_open", "session", id, "network", "udp", "transport", "quic-datagram", "destination", req.Address)
	var up, down atomic.Int64
	defer func() {
		s.Log.Info("flow_bytes", "session", id, "network", "udp", "destination", req.Address, "up", up.Load(), "down", down.Load())
	}()
	touch := func() { t := time.Now().Add(60 * time.Second); f.SetDeadline(t); out.SetDeadline(t) }
	touch()
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
			if _, e = f.Write(b[:n]); e != nil {
				return
			}
			down.Add(int64(n))
			if count != nil {
				count(0, n)
			}
			touch()
		}
	}()
	defer func() { stop(); out.Close(); <-done }()
	b := make([]byte, maxUDP)
	for {
		n, e := f.Read(b)
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

var _ net.Conn = (*DatagramFlow)(nil)
