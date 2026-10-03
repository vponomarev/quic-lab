package vless

import (
	"bytes"
	"context"
	"errors"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/proxy"
	vencoding "github.com/xtls/xray-core/proxy/vless/encoding"
	"io"
	"os"
	"testing"
	"time"
)

func TestFlowDeadlinesAndHalfClose(t *testing.T) {
	c, link := newFlow(context.Background(), false)
	defer c.Close()
	done := make(chan error, 1)
	go func() { _, err := c.Read(make([]byte, 1)); done <- err }()
	c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("read deadline ignored")
	}
	c.SetReadDeadline(time.Time{})
	b := buf.New()
	b.Write([]byte("reply"))
	if err := link.Writer.WriteMultiBuffer(buf.MultiBuffer{b}); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 5)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "reply" {
		t.Fatalf("read after deadline: %q %v", got, err)
	}
	if err := c.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if _, err := link.Reader.ReadMultiBuffer(); !errors.Is(err, io.EOF) {
		t.Fatalf("half close %v", err)
	}
	b = buf.New()
	b.Write([]byte("tail"))
	if err := link.Writer.WriteMultiBuffer(buf.MultiBuffer{b}); err != nil {
		t.Fatal(err)
	}
	got = make([]byte, 4)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "tail" {
		t.Fatalf("half close lost reply: %q %v", got, err)
	}
}
func TestFlowWriteDeadlineBackpressureAndReset(t *testing.T) {
	c, link := newFlow(context.Background(), false)
	defer c.Close()
	c.SetWriteDeadline(time.Now().Add(20 * time.Millisecond))
	n, err := c.Write(make([]byte, 256*1024))
	if n >= 256*1024 || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("unbounded or no deadline: %d %v", n, err)
	}
	c.SetWriteDeadline(time.Time{})
	mb, err := link.Reader.ReadMultiBuffer()
	if err != nil {
		t.Fatal(err)
	}
	buf.ReleaseMulti(mb)
	if _, err = c.Write([]byte("again")); err != nil {
		t.Fatal(err)
	}
}
func TestFlowUDPPacketBoundaries(t *testing.T) {
	c, link := newFlow(context.Background(), true)
	defer c.Close()
	for _, size := range []int{1, 2048, 8190} {
		payload := bytes.Repeat([]byte{byte(size % 251)}, size)
		if n, err := c.Write(payload); err != nil || n != size {
			t.Fatalf("write %d %d %v", size, n, err)
		}
		mb, err := link.Reader.ReadMultiBuffer()
		if err != nil {
			t.Fatal(err)
		}
		if len(mb) != 1 || !bytes.Equal(mb[0].Bytes(), payload) {
			t.Fatal("packet fragmented")
		}
		if err = link.Writer.WriteMultiBuffer(mb); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, 65507)
		n, err := c.Read(got)
		if err != nil || !bytes.Equal(got[:n], payload) {
			t.Fatalf("packet altered %d %d %v", size, n, err)
		}
	}
	if _, err := c.Write(make([]byte, 8191)); err == nil {
		t.Fatal("oversize UDP accepted")
	}
}
func TestFlowCloseUnblocksAndDiscards(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c, _ := newFlow(ctx, false)
	done := make(chan error, 1)
	go func() { _, err := c.Read(make([]byte, 1)); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("nil close error")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel leaked read")
	}
	c.Close()
	c.Close()
}

func TestFlowRejectsOversizedUDPReplyBeforeFragments(t *testing.T) {
	c, link := newFlow(context.Background(), true)
	defer c.Close()
	// A single LengthPacketReader call splits a 9000-byte packet into these two
	// buffers; admitting either one would fabricate a different datagram.
	wire := append([]byte{0x23, 0x28}, make([]byte, 9000)...)
	reader := vencoding.NewLengthPacketReader(bytes.NewReader(wire))
	mb, err := reader.ReadMultiBuffer()
	if err != nil {
		t.Fatal(err)
	}
	if len(mb) != 2 {
		buf.ReleaseMulti(mb)
		t.Fatal("upstream packet split changed")
	}
	if err := link.Writer.WriteMultiBuffer(mb); err == nil {
		t.Fatal("oversized reply accepted as fragmented datagrams")
	}
	c.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	n, err := c.Read(make([]byte, 9000))
	if n != 0 || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("reply fragment escaped: %d %v", n, err)
	}
}

func TestFlowTCPBuffersSurviveVisionReshape(t *testing.T) {
	c, link := newFlow(context.Background(), false)
	defer c.Close()
	payload := bytes.Repeat([]byte{42}, 16*1024)
	if _, err := c.Write(payload); err != nil {
		t.Fatal(err)
	}
	mb, err := link.Reader.ReadMultiBuffer()
	if err != nil {
		t.Fatal(err)
	}
	mb = proxy.ReshapeMultiBuffer(context.Background(), mb)
	defer buf.ReleaseMulti(mb)
	var got []byte
	for _, b := range mb {
		got = append(got, b.Bytes()...)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("Vision reshape lost bytes: %d of %d", len(got), len(payload))
	}
}
