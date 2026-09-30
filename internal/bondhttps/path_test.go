package bondhttps

import (
	"bytes"
	"context"
	"fmt"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPSPathFramingAndCancel(t *testing.T) {
	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer c.CloseNow()
		kind, b, e := c.Read(r.Context())
		if e != nil {
			return
		}
		if kind != websocket.MessageBinary {
			return
		}
		received <- b
		c.Write(r.Context(), kind, b)
		c.Read(r.Context())
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if e != nil {
		t.Fatal(e)
	}
	cleaned := make(chan struct{})
	p := NewPath(c, func() { close(cleaned) })
	defer p.Close()
	packet := bytes.Repeat([]byte{7}, 1080)
	if e = p.SendDatagram(packet); e != nil {
		t.Fatal(e)
	}
	select {
	case got := <-received:
		if !bytes.Equal(got, packet) {
			t.Fatal("framing changed")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	got, e := p.ReceiveDatagram(ctx)
	if e != nil || !bytes.Equal(got, packet) {
		t.Fatal("round trip", e)
	}
	if p.SendDatagram(make([]byte, 1081)) == nil {
		t.Fatal("oversized record accepted")
	}
	p.Close()
	p.Close()
	select {
	case <-cleaned:
	case <-ctx.Done():
		t.Fatal("cleanup missing")
	}
	if _, e = p.ReceiveDatagram(ctx); e == nil {
		t.Fatal("closed path read succeeded")
	}
}

type blockedSocket struct{ started chan struct{} }

func (b *blockedSocket) SetReadLimit(int64) {}
func (b *blockedSocket) CloseNow() error    { return nil }
func (b *blockedSocket) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	<-ctx.Done()
	return 0, nil, ctx.Err()
}
func (b *blockedSocket) Write(ctx context.Context, _ websocket.MessageType, _ []byte) error {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}
func TestBlockedHTTPSQueueIsNonBlocking(t *testing.T) {
	socket := &blockedSocket{started: make(chan struct{}, 1)}
	p := newPath(socket, nil)
	defer p.Close()
	packet := make([]byte, 30)
	if e := p.SendDatagram(packet); e != nil {
		t.Fatal(e)
	}
	select {
	case <-socket.started:
	case <-time.After(time.Second):
		t.Fatal("writer not started")
	}
	done := make(chan int, 1)
	go func() {
		rejected := 0
		for i := 0; i < 100; i++ {
			if p.SendDatagram(packet) != nil {
				rejected++
			}
		}
		done <- rejected
	}()
	select {
	case rejected := <-done:
		if rejected != 68 {
			t.Fatalf("queue cap: rejected %d", rejected)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked writer blocked sender")
	}
}
func TestRejectsInvalidInboundFrame(t *testing.T) {
	for _, kind := range []websocket.MessageType{websocket.MessageText, websocket.MessageBinary} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, e := websocket.Accept(w, r, nil)
				if e != nil {
					return
				}
				defer c.CloseNow()
				size := 1081
				if kind == websocket.MessageText {
					size = 30
				}
				c.Write(r.Context(), kind, make([]byte, size))
				c.Read(r.Context())
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			c, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			if e != nil {
				t.Fatal(e)
			}
			p := NewPath(c, nil)
			defer p.Close()
			if _, e = p.ReceiveDatagram(ctx); e == nil {
				t.Fatal("invalid inbound frame accepted")
			}
		})
	}
}
