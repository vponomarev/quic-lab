// Package bondhttps carries one bounded bond record per binary WebSocket message.
package bondhttps

import (
	"context"
	"errors"
	"github.com/coder/websocket"
	"quiclab/internal/bond"
	"sync"
	"time"
)

const Subprotocol = "quic-lab-bond-v1"
const MaxRecordSize = bond.Chunk + 30

type socket interface {
	Write(context.Context, websocket.MessageType, []byte) error
	Read(context.Context) (websocket.MessageType, []byte, error)
	CloseNow() error
	SetReadLimit(int64)
}
type path struct {
	conn    socket
	ctx     context.Context
	cancel  context.CancelFunc
	queue   chan []byte
	once    sync.Once
	cleanup func()
}

func NewPath(c *websocket.Conn, cleanup func()) bond.Path { return newPath(c, cleanup) }
func newPath(c socket, cleanup func()) *path {
	ctx, cancel := context.WithCancel(context.Background())
	p := &path{conn: c, ctx: ctx, cancel: cancel, queue: make(chan []byte, 32), cleanup: cleanup}
	c.SetReadLimit(MaxRecordSize)
	go func() {
		defer p.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case b := <-p.queue:
				writeCtx, stop := context.WithTimeout(ctx, 3*time.Second)
				err := c.Write(writeCtx, websocket.MessageBinary, b)
				stop()
				if err != nil {
					return
				}
			}
		}
	}()
	return p
}
func (p *path) SendDatagram(b []byte) error {
	if len(b) < 30 || len(b) > MaxRecordSize {
		return errors.New("invalid bond record size")
	}
	if p.ctx.Err() != nil {
		return p.ctx.Err()
	}
	select {
	case p.queue <- append([]byte(nil), b...):
		return nil
	default:
		return errors.New("path queue full")
	}
}
func (p *path) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()
	if p.ctx.Err() != nil {
		return nil, p.ctx.Err()
	}
	kind, b, err := p.conn.Read(readCtx)
	if err == nil && (kind != websocket.MessageBinary || len(b) < 30 || len(b) > MaxRecordSize) {
		err = errors.New("invalid bond record")
	}
	if err != nil {
		p.Close()
		return nil, err
	}
	return b, nil
}
func (p *path) Close() error {
	p.once.Do(func() {
		p.cancel()
		p.conn.CloseNow()
		if p.cleanup != nil {
			p.cleanup()
		}
	})
	return nil
}
