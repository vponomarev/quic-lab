package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

type fakeDatagrams struct {
	ctx      context.Context
	incoming chan []byte
}

func (f *fakeDatagrams) Context() context.Context    { return f.ctx }
func (f *fakeDatagrams) SendDatagram(b []byte) error { return nil }
func (f *fakeDatagrams) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	select {
	case b := <-f.incoming:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (f *fakeDatagrams) LocalAddr() net.Addr  { return &net.UDPAddr{} }
func (f *fakeDatagrams) RemoteAddr() net.Addr { return &net.UDPAddr{} }
func fragment(flow uint64, seq uint32, index, total int, b []byte) []byte {
	p := make([]byte, 16+len(b))
	binary.BigEndian.PutUint64(p, flow)
	binary.BigEndian.PutUint32(p[8:], seq)
	binary.BigEndian.PutUint16(p[12:], uint16(index))
	binary.BigEndian.PutUint16(p[14:], uint16(total))
	copy(p[16:], b)
	return p
}
func TestDatagramLossReorderIsolationAndDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &fakeDatagrams{ctx: ctx, incoming: make(chan []byte, 32)}
	m := newDatagramMux(c)
	a, _ := m.Register(4)
	defer a.Close()
	b, _ := m.Register(8)
	defer b.Close()
	// Lose the second fragment of packet 1. Packet 2 must not wait for it.
	c.incoming <- fragment(4, 1, 0, 2, bytes.Repeat([]byte{1}, 1000))
	c.incoming <- fragment(4, 2, 0, 1, []byte("fresh"))
	c.incoming <- fragment(8, 2, 0, 1, []byte("other-flow"))
	// Packet 3 fragments arrive out of order.
	c.incoming <- fragment(4, 3, 1, 2, []byte("tail"))
	c.incoming <- fragment(4, 3, 0, 2, bytes.Repeat([]byte{3}, 1000))
	buf := make([]byte, 2000)
	a.SetDeadline(time.Now().Add(time.Second))
	n, e := a.Read(buf)
	if e != nil || string(buf[:n]) != "fresh" {
		t.Fatalf("loss blocked next packet %d %v", n, e)
	}
	n, e = a.Read(buf)
	if e != nil || n != 1004 || string(buf[1000:n]) != "tail" {
		t.Fatalf("reassembly %d %v", n, e)
	}
	b.SetDeadline(time.Now().Add(time.Second))
	n, e = b.Read(buf)
	if e != nil || string(buf[:n]) != "other-flow" {
		t.Fatal("flow mixing", e)
	}
	a.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	_, e = a.Read(buf)
	if !errors.Is(e, os.ErrDeadlineExceeded) {
		t.Fatal(e)
	}
	a.SetReadDeadline(time.Time{})
	done := make(chan error, 1)
	go func() { _, e := a.Read(buf); done <- e }()
	a.Close()
	select {
	case e := <-done:
		if !errors.Is(e, net.ErrClosed) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("close leaked read")
	}
	c.incoming <- fragment(999, 1, 0, 1, []byte("unauthorized"))
}
func TestDatagramQueueAndAssembliesBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &fakeDatagrams{ctx: ctx}
	m := &DatagramMux{conn: c, flows: make(map[uint64]*DatagramFlow), send: make(chan queuedDatagram, 1)}
	f, _ := m.Register(0)
	defer f.Close()
	for i := 0; i < 100; i++ {
		f.Write([]byte("no receiver"))
		f.accept(fragment(0, uint32(i), 0, 2, make([]byte, 1000)))
	}
	if len(m.send) != 1 || len(f.parts) > 8 || m.Drops.Load() == 0 {
		t.Fatal("unbounded queue")
	}
}
