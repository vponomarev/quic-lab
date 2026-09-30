package bond

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Mux keeps flow state independent of paths. Only the client opens streams.
// Lifetime IDs are never reused; tombstones prevent delayed OPEN resurrection.
type Mux struct {
	buffered  atomic.Int32
	Session   *Session
	mu        sync.Mutex
	client    bool
	next      uint32
	flows     map[uint32]*Stream
	closed    map[uint32]bool
	accepted  chan *Stream
	datagrams chan []byte
}

func NewMux(ctx context.Context, client bool) *Mux {
	m := &Mux{client: client, flows: map[uint32]*Stream{}, closed: map[uint32]bool{}, accepted: make(chan *Stream, 128), datagrams: make(chan []byte, 128)}
	m.Session = New(ctx, m.deliver)
	return m
}
func (m *Mux) deliver(r Record) bool {
	if r.Kind == Datagram {
		select {
		case m.datagrams <- r.Payload:
			return true
		default:
			return true
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed[r.Flow] {
		return true
	}
	f := m.flows[r.Flow]
	if r.Kind == Open {
		if m.client || r.Flow == 0 {
			return false
		}
		if f != nil {
			return true
		}
		if len(m.flows) >= 128 || len(m.closed) >= 65536 {
			return false
		}
		f = m.newStream(r.Flow)
		m.flows[r.Flow] = f
		select {
		case m.accepted <- f:
			return true
		default:
			delete(m.flows, r.Flow)
			return false
		}
	}
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Kind == Stop {
		f.cancelWrite()
		return true
	}
	if r.Kind == Reset {
		f.cancelWrite()
		f.err = net.ErrClosed
		f.signal()
		return true
	}
	if r.Kind != Data && r.Kind != Fin {
		return false
	}
	if r.Seq < f.recvNext || f.readClosed {
		m.Session.noteDuplicate()
		return true
	}
	if _, ok := f.recv[r.Seq]; ok {
		m.Session.noteDuplicate()
		return true
	}
	// Credit is implicit: no ACK while the bounded receiver is full.
	if len(f.recv) >= 128 || r.Seq >= f.recvNext+128 {
		return false
	}
	if m.buffered.Add(1) > 2048 {
		m.buffered.Add(-1)
		return false
	}
	f.recv[r.Seq] = r
	f.signal()
	return true
}
func (m *Mux) newStream(id uint32) *Stream {
	ctx, cancel := context.WithCancel(m.Context())
	return &Stream{writeCtx: ctx, cancelWrite: cancel, m: m, id: id, recv: map[uint64]Record{}, changed: make(chan struct{}), recvNext: 1, sendNext: 1}
}
func (m *Mux) OpenStream(ctx context.Context) (*Stream, error) {
	m.mu.Lock()
	if !m.client || len(m.flows) >= 128 || m.next >= 65536 {
		m.mu.Unlock()
		return nil, errors.New("bond flow limit")
	}
	m.next++
	f := m.newStream(m.next)
	m.flows[f.id] = f
	m.mu.Unlock()
	if e := m.Session.Send(ctx, Record{Kind: Open, Flow: f.id}); e != nil {
		m.mu.Lock()
		delete(m.flows, f.id)
		m.mu.Unlock()
		return nil, e
	}
	return f, nil
}
func (m *Mux) AcceptStream(ctx context.Context) (*Stream, error) {
	select {
	case f := <-m.accepted:
		return f, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.Session.Context().Done():
		return nil, net.ErrClosed
	}
}
func (m *Mux) Context() context.Context { return m.Session.Context() }
func (m *Mux) Close() error             { return m.Session.Close() }
func (m *Mux) SendDatagram(b []byte) error {
	// Gateway datagrams can be up to 1016 bytes; fragment the overlay payload.
	if len(b) > Chunk {
		return errors.New("bond datagram exceeds record MTU")
	}
	return m.Session.Send(m.Context(), Record{Kind: Datagram, Payload: b})
}
func (m *Mux) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	select {
	case b := <-m.datagrams:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.Context().Done():
		return nil, net.ErrClosed
	}
}

type address string

func (a address) Network() string   { return "bond" }
func (a address) String() string    { return string(a) }
func (m *Mux) LocalAddr() net.Addr  { return address("bond-local") }
func (m *Mux) RemoteAddr() net.Addr { return address("bond-peer") }

type Stream struct {
	writeCtx                context.Context
	cancelWrite             context.CancelFunc
	closeOnce               sync.Once
	m                       *Mux
	id                      uint32
	mu                      sync.Mutex
	writeMu                 sync.Mutex
	recv                    map[uint64]Record
	recvNext, sendNext      uint64
	partial                 []byte
	readClosed, writeClosed bool
	err                     error
	deadline                time.Time
	changed                 chan struct{}
}

func (f *Stream) FlowID() uint64 { return uint64(f.id) }
func (f *Stream) signal()        { close(f.changed); f.changed = make(chan struct{}) }
func (f *Stream) SetDeadline(t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deadline = t
	f.signal()
	return nil
}
func (f *Stream) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	for {
		f.mu.Lock()
		if len(f.partial) > 0 {
			n := copy(b, f.partial)
			f.partial = f.partial[n:]
			f.mu.Unlock()
			return n, nil
		}
		if f.err != nil {
			e := f.err
			f.mu.Unlock()
			return 0, e
		}
		if f.readClosed {
			f.mu.Unlock()
			return 0, io.EOF
		}
		if r, ok := f.recv[f.recvNext]; ok {
			delete(f.recv, f.recvNext)
			f.m.buffered.Add(-1)
			f.recvNext++
			if r.Kind == Fin {
				f.readClosed = true
				f.mu.Unlock()
				return 0, io.EOF
			}
			f.partial = r.Payload
			f.mu.Unlock()
			continue
		}
		changed, deadline := f.changed, f.deadline
		f.mu.Unlock()
		var timeout <-chan time.Time
		var timer *time.Timer
		if !deadline.IsZero() {
			if !time.Now().Before(deadline) {
				return 0, os.ErrDeadlineExceeded
			}
			timer = time.NewTimer(time.Until(deadline))
			timeout = timer.C
		}
		select {
		case <-changed:
		case <-timeout:
			if timer != nil {
				timer.Stop()
			}
			return 0, os.ErrDeadlineExceeded
		case <-f.m.Context().Done():
			if timer != nil {
				timer.Stop()
			}
			return 0, net.ErrClosed
		}
		if timer != nil {
			timer.Stop()
		}
	}
}
func (f *Stream) send(kind byte, b []byte) error {
	f.mu.Lock()
	if f.err != nil {
		e := f.err
		f.mu.Unlock()
		return e
	}
	deadline := f.deadline
	seq := f.sendNext
	f.mu.Unlock()
	ctx := f.writeCtx
	cancel := func() {}
	if !deadline.IsZero() {
		ctx, cancel = context.WithDeadline(ctx, deadline)
	}
	defer cancel()
	e := f.m.Session.Send(ctx, Record{Kind: kind, Flow: f.id, Seq: seq, Payload: b})
	if e == nil {
		f.mu.Lock()
		f.sendNext++
		f.mu.Unlock()
	}
	return e
}
func (f *Stream) Write(b []byte) (int, error) {
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	if f.writeClosed {
		return 0, net.ErrClosed
	}
	total := 0
	for len(b) > 0 {
		n := min(len(b), Chunk)
		if e := f.send(Data, b[:n]); e != nil {
			return total, e
		}
		total += n
		b = b[n:]
	}
	return total, nil
}
func (f *Stream) CloseWrite() error {
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	if f.writeClosed {
		return nil
	}
	f.writeClosed = true
	return f.send(Fin, nil)
}
func (f *Stream) Close() error {
	f.closeOnce.Do(func() {
		// Cancel a blocked writer, but preserve records already accepted for delivery.
		f.cancelWrite()
		f.writeMu.Lock()
		ctx, cancel := context.WithTimeout(f.m.Context(), time.Second)
		if !f.writeClosed {
			f.writeClosed = true
			f.mu.Lock()
			seq := f.sendNext
			f.mu.Unlock()
			f.m.Session.Send(ctx, Record{Kind: Fin, Flow: f.id, Seq: seq})
		}
		// STOP is directional: it cancels the peer's writes without discarding our reply.
		f.m.Session.Send(ctx, Record{Kind: Stop, Flow: f.id})
		cancel()
		f.writeMu.Unlock()
		f.mu.Lock()
		f.readClosed = true
		f.m.buffered.Add(-int32(len(f.recv)))
		f.recv = map[uint64]Record{}
		f.partial = nil
		f.signal()
		f.mu.Unlock()
		f.m.mu.Lock()
		delete(f.m.flows, f.id)
		f.m.closed[f.id] = true
		f.m.mu.Unlock()
	})
	return nil
}
