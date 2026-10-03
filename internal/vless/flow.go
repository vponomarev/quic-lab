package vless

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/transport"
)

const flowQueueLimit = 64 * 1024

// Xray VLESS MultiLengthPacketWriter requires payload + two-byte length <= buf.Size.
const maxDatagram = buf.Size - 2

type flowDeadline struct {
	mu      sync.Mutex
	at      time.Time
	changed chan struct{}
}

func (d *flowDeadline) set(at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.changed != nil {
		close(d.changed)
	}
	d.changed = make(chan struct{})
	d.at = at
}
func (d *flowDeadline) snapshot() (time.Time, <-chan struct{}) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.changed == nil {
		d.changed = make(chan struct{})
	}
	return d.at, d.changed
}
func (d *flowDeadline) expired() bool {
	at, _ := d.snapshot()
	return !at.IsZero() && !time.Now().Before(at)
}
func waitFlow(ch <-chan struct{}, d *flowDeadline) error {
	at, changed := d.snapshot()
	var timer *time.Timer
	var timeout <-chan time.Time
	if !at.IsZero() {
		remaining := time.Until(at)
		if remaining <= 0 {
			return os.ErrDeadlineExceeded
		}
		timer = time.NewTimer(remaining)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case <-ch:
		return nil
	case <-changed:
		return nil
	case <-timeout:
		return os.ErrDeadlineExceeded
	}
}

// One queue retains at most 64 KiB. A writer transfers ownership of each buffer
// only on admission. Deadlines never leave a background write running.
type flowQueue struct {
	mu                sync.Mutex
	data              buf.MultiBuffer
	size              int
	closed            error
	changed           chan struct{}
	unlimitedDeadline flowDeadline
}

func newFlowQueue() *flowQueue { return &flowQueue{changed: make(chan struct{})} }
func (q *flowQueue) signal()   { close(q.changed); q.changed = make(chan struct{}) }
func (q *flowQueue) put(b *buf.Buffer, d *flowDeadline) error {
	if int(b.Len()) > flowQueueLimit {
		return errors.New("VLESS buffer exceeds queue limit")
	}
	for {
		if d.expired() {
			return os.ErrDeadlineExceeded
		}
		q.mu.Lock()
		if q.closed != nil {
			q.mu.Unlock()
			return io.ErrClosedPipe
		}
		if q.size+int(b.Len()) <= flowQueueLimit {
			q.data = append(q.data, b)
			q.size += int(b.Len())
			q.signal()
			q.mu.Unlock()
			return nil
		}
		ch := q.changed
		q.mu.Unlock()
		if err := waitFlow(ch, d); err != nil {
			return err
		}
	}
}
func (q *flowQueue) WriteMultiBuffer(mb buf.MultiBuffer) error {
	for i, b := range mb {
		if err := q.put(b, &q.unlimitedDeadline); err != nil {
			buf.ReleaseMulti(mb[i:])
			return err
		}
	}
	return nil
}
func (q *flowQueue) ReadMultiBuffer() (buf.MultiBuffer, error) {
	for {
		q.mu.Lock()
		if len(q.data) > 0 {
			mb := q.data
			q.data = nil
			q.size = 0
			q.signal()
			q.mu.Unlock()
			return mb, nil
		}
		if q.closed != nil {
			err := q.closed
			q.mu.Unlock()
			return nil, err
		}
		ch := q.changed
		q.mu.Unlock()
		<-ch
	}
}
func (q *flowQueue) read(p []byte, packet bool, d *flowDeadline) (int, error) {
	for {
		if d.expired() {
			return 0, os.ErrDeadlineExceeded
		}
		q.mu.Lock()
		if len(q.data) > 0 {
			b := q.data[0]
			n := copy(p, b.Bytes())
			old := int(b.Len())
			if packet || n == old {
				b.Release()
				q.data[0] = nil
				q.data = q.data[1:]
				q.size -= old
			} else {
				b.Advance(int32(n))
				q.size -= n
			}
			q.signal()
			q.mu.Unlock()
			return n, nil
		}
		if q.closed != nil {
			err := q.closed
			q.mu.Unlock()
			return 0, err
		}
		ch := q.changed
		q.mu.Unlock()
		if err := waitFlow(ch, d); err != nil {
			return 0, err
		}
	}
}
func (q *flowQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed == nil {
		q.closed = io.EOF
		q.signal()
	}
	return nil
}
func (q *flowQueue) Interrupt() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = net.ErrClosed
	buf.ReleaseMulti(q.data)
	q.data = nil
	q.size = 0
	q.signal()
}

type flowConn struct {
	up, down        *flowQueue
	packet          bool
	rd, wd          flowDeadline
	readMu, writeMu sync.Mutex
	once            sync.Once
	stopMu          sync.Mutex
	stop            func() bool
	onClose         func()
}

func newFlow(ctx context.Context, packet bool) (*flowConn, *transport.Link) {
	return newFlowWithClose(ctx, packet, nil)
}
func newFlowWithClose(ctx context.Context, packet bool, onClose func()) (*flowConn, *transport.Link) {
	c := &flowConn{up: newFlowQueue(), down: newFlowQueue(), packet: packet, onClose: onClose}
	c.stopMu.Lock()
	c.stop = context.AfterFunc(ctx, func() { c.Close() })
	c.stopMu.Unlock()
	var downstream buf.Writer = c.down
	if packet {
		downstream = &flowPacketWriter{c.down}
	}
	return c, &transport.Link{Reader: c.up, Writer: downstream}
}
func (c *flowConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	return c.down.read(p, c.packet, &c.rd)
}
func (c *flowConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.packet && len(p) > maxDatagram {
		return 0, errors.New("VLESS datagram exceeds transport limit")
	}
	n := 0
	for len(p) > 0 {
		size := len(p)
		if !c.packet && size > buf.Size {
			size = buf.Size
		}
		b := buf.NewWithSize(int32(size))
		b.Write(p[:size])
		if err := c.up.put(b, &c.wd); err != nil {
			b.Release()
			return n, err
		}
		n += size
		p = p[size:]
	}
	return n, nil
}
func (c *flowConn) CloseWrite() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.up.Close()
}
func (c *flowConn) Close() error {
	c.once.Do(func() {
		c.stopMu.Lock()
		if c.stop != nil {
			c.stop()
		}
		c.stopMu.Unlock()
		c.up.Interrupt()
		c.down.Interrupt()
		if c.onClose != nil {
			c.onClose()
		}
	})
	return nil
}
func (c *flowConn) LocalAddr() net.Addr                { return flowAddr("vless-local") }
func (c *flowConn) RemoteAddr() net.Addr               { return flowAddr("vless-remote") }
func (c *flowConn) SetDeadline(t time.Time) error      { c.rd.set(t); c.wd.set(t); return nil }
func (c *flowConn) SetReadDeadline(t time.Time) error  { c.rd.set(t); return nil }
func (c *flowConn) SetWriteDeadline(t time.Time) error { c.wd.set(t); return nil }

type flowAddr string

func (a flowAddr) Network() string { return "vless" }
func (a flowAddr) String() string  { return string(a) }

// The restricted VLESS LengthPacketReader and XUDP PacketReader each return one
// datagram per ReadMultiBuffer call. LengthPacketReader may split an oversized
// packet into several buffers: reject the whole call before enqueuing fragments.
type flowPacketWriter struct{ *flowQueue }

func (w *flowPacketWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	if mb.Len() > maxDatagram || len(mb) > 1 {
		buf.ReleaseMulti(mb)
		return errors.New("VLESS reply datagram exceeds transport limit")
	}
	if mb.IsEmpty() {
		buf.ReleaseMulti(mb)
		return nil
	}
	return w.flowQueue.WriteMultiBuffer(mb)
}
