package bondquic

import (
	"context"
	"errors"
	"github.com/quic-go/quic-go"
	"sync"
)

const ALPN = "quic-lab-bond/2"

// Queue isolates each path's congestion controller; it cannot block the other path.
type QUICPath struct {
	conn    *quic.Conn
	queue   chan []byte
	once    sync.Once
	cleanup func()
}

func NewQUICPath(c *quic.Conn, cleanup func()) *QUICPath {
	p := &QUICPath{conn: c, queue: make(chan []byte, 32), cleanup: cleanup}
	go func() {
		defer p.Close()
		for {
			select {
			case <-c.Context().Done():
				return
			case b := <-p.queue:
				if c.SendDatagram(b) != nil {
					return
				}
			}
		}
	}()
	return p
}
func (p *QUICPath) SendDatagram(b []byte) error {
	select {
	case <-p.conn.Context().Done():
		return context.Canceled
	default:
	}
	select {
	case p.queue <- append([]byte(nil), b...):
		return nil
	default:
		return errors.New("path queue full")
	}
}
func (p *QUICPath) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	return p.conn.ReceiveDatagram(ctx)
}
func (p *QUICPath) Close() error {
	p.once.Do(func() {
		p.conn.CloseWithError(0, "path closed")
		if p.cleanup != nil {
			p.cleanup()
		}
	})
	return nil
}
