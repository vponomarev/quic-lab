package awgserver

import (
	"context"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/tun"
)

type ExitRoute struct {
	ID       string
	Managed  bool
	Revision string
}
type exitPacket struct {
	data  []byte
	owner *exitSession
}
type ExitTUN struct {
	tun.Device
	open     func(context.Context, string) (net.Conn, error)
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	closed   bool
	routes   map[netip.Addr]ExitRoute
	sessions map[netip.Addr]*exitSession
	packets  chan exitPacket
	wg       sync.WaitGroup
	once     sync.Once
}
type exitSession struct {
	router *ExitTUN
	route  ExitRoute
	ctx    context.Context
	cancel context.CancelFunc
	queue  chan []byte
	done   chan struct{}
}

// ExitTUN is inside AdmissionTUN: only decrypted, authenticated peer packets can
// reach Write. Legacy peers keep kernel routing; managed peers never touch it.
func NewExitTUN(inner tun.Device, open func(context.Context, string) (net.Conn, error)) *ExitTUN {
	ctx, cancel := context.WithCancel(context.Background())
	r := &ExitTUN{Device: inner, open: open, ctx: ctx, cancel: cancel, routes: map[netip.Addr]ExitRoute{}, sessions: map[netip.Addr]*exitSession{}, packets: make(chan exitPacket, 128)}
	r.wg.Add(1)
	go r.readKernel()
	return r
}
func (r *ExitTUN) BatchSize() int { return 1 }
func (r *ExitTUN) Update(routes map[netip.Addr]ExitRoute) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	next := make(map[netip.Addr]ExitRoute, len(routes))
	for ip, route := range routes {
		next[ip] = route
	}
	var removed []*exitSession
	for ip, s := range r.sessions {
		if route, ok := next[ip]; !ok || route != s.route {
			removed = append(removed, s)
			delete(r.sessions, ip)
		}
	}
	r.routes = next
	r.mu.Unlock()
	for _, s := range removed {
		s.cancel()
	}
	for _, s := range removed {
		<-s.done
	}
}
func (r *ExitTUN) Write(bufs [][]byte, offset int) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.ctx.Err() != nil {
		return 0, net.ErrClosed
	}
	for _, buf := range bufs {
		if offset < 0 || offset > len(buf) {
			continue
		}
		packet := buf[offset:]
		ip, ok := packetAddress(packet, true)
		if !ok {
			continue
		}
		route, ok := r.routes[ip]
		if !ok {
			continue
		}
		if !route.Managed {
			if _, err := r.Device.Write([][]byte{buf}, offset); err != nil {
				return 0, err
			}
			continue
		}
		s := r.sessions[ip]
		if s == nil {
			ctx, cancel := context.WithCancel(r.ctx)
			s = &exitSession{router: r, route: route, ctx: ctx, cancel: cancel, queue: make(chan []byte, 128), done: make(chan struct{})}
			r.sessions[ip] = s
			r.wg.Add(1)
			go s.run()
		}
		select {
		case s.queue <- append([]byte(nil), packet...):
		default:
		}
	}
	return len(bufs), nil
}
func (r *ExitTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	for {
		if r.ctx.Err() != nil {
			return 0, net.ErrClosed
		}
		select {
		case <-r.ctx.Done():
			return 0, net.ErrClosed
		case p := <-r.packets:
			ip, ok := packetAddress(p.data, false)
			if !ok {
				continue
			}
			r.mu.Lock()
			route, present := r.routes[ip]
			valid := present && ((!route.Managed && p.owner == nil) || (route.Managed && p.owner != nil && r.sessions[ip] == p.owner))
			r.mu.Unlock()
			if !valid {
				continue
			}
			if len(bufs) == 0 || len(sizes) == 0 || offset < 0 || offset > len(bufs[0]) || len(bufs[0])-offset < len(p.data) {
				return 0, io.ErrShortBuffer
			}
			sizes[0] = copy(bufs[0][offset:], p.data)
			return 1, nil
		}
	}
}
func (r *ExitTUN) enqueue(p []byte, owner *exitSession) {
	select {
	case r.packets <- exitPacket{append([]byte(nil), p...), owner}:
	default:
	}
}
func (r *ExitTUN) readKernel() {
	defer r.wg.Done()
	count := r.Device.BatchSize()
	if count < 1 {
		count = 1
	}
	bufs := make([][]byte, count)
	sizes := make([]int, count)
	for i := range bufs {
		bufs[i] = make([]byte, 65535)
	}
	for {
		n, err := r.Device.Read(bufs, sizes, 0)
		if err != nil {
			r.cancel()
			return
		}
		for i := 0; i < n; i++ {
			if sizes[i] > 0 && sizes[i] <= len(bufs[i]) {
				r.enqueue(bufs[i][:sizes[i]], nil)
			}
		}
	}
}
func (r *ExitTUN) Close() error {
	r.once.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.cancel()
		r.mu.Unlock()
		r.Device.Close()
		r.wg.Wait()
	})
	return nil
}
func (s *exitSession) run() {
	defer s.router.wg.Done()
	defer close(s.done)
	for {
		select {
		case <-s.ctx.Done():
			return
		case first := <-s.queue:
			s.exchange(first)
			timer := time.NewTimer(time.Second)
			select {
			case <-s.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}
func (s *exitSession) exchange(first []byte) {
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	c, err := s.router.open(ctx, s.route.ID)
	cancel()
	if err != nil {
		if c != nil {
			c.Close()
		}
		return
	}
	stop := context.AfterFunc(s.ctx, func() { c.Close() })
	defer stop()
	defer c.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65535)
		for {
			n, err := c.Read(buf)
			if err != nil {
				return
			}
			s.router.enqueue(buf[:n], s)
		}
	}()
	defer func() { c.Close(); <-done }()
	packet := first
	for {
		c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if _, err = c.Write(packet); err != nil {
			return
		}
		select {
		case <-s.ctx.Done():
			return
		case <-done:
			return
		case packet = <-s.queue:
		}
	}
}
