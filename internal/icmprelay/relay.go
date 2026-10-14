package icmprelay

import (
	"context"
	"net/netip"
	"sync"
	"time"
)

type Exchange func(context.Context, netip.Addr, []byte) ([]byte, error)
type Relay struct {
	ctx    context.Context
	cancel context.CancelFunc
	queue  chan []byte
	wg     sync.WaitGroup
}

func New(exchange Exchange, write func([]byte) error) *Relay {
	return NewPacket(func(ctx context.Context, p []byte) ([]byte, error) {
		d, q, _ := Parse(p)
		m, e := exchange(ctx, d, q)
		if e != nil {
			return nil, e
		}
		return Reply(p, m), nil
	}, write)
}
func NewPacket(exchange func(context.Context, []byte) ([]byte, error), write func([]byte) error) *Relay {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Relay{ctx: ctx, cancel: cancel, queue: make(chan []byte, 32)}
	for i := 0; i < 8; i++ {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case packet := <-r.queue:
					if ctx.Err() != nil {
						return
					}
					_, _, ok := Parse(packet)
					if !ok {
						continue
					}
					probe, stop := context.WithTimeout(ctx, 3*time.Second)
					reply, e := exchange(probe, packet)
					stop()
					if e == nil {
						if ValidateResponse(packet, reply) {
							write(reply)
						}
					}
				}
			}
		}()
	}
	return r
}

// All IPv4 ICMP is consumed, including malformed Echo. It must never reach a
// userspace stack that could synthesize a successful reply without exit traffic.
func (r *Relay) Handle(packet []byte) bool {
	if len(packet) < 20 || packet[0]>>4 != 4 || packet[9] != 1 {
		return false
	}
	if _, _, ok := Parse(packet); ok {
		select {
		case <-r.ctx.Done():
		case r.queue <- append([]byte(nil), packet...):
		default:
		}
	}
	return true
}
func (r *Relay) Close() { r.cancel(); r.wg.Wait() }
