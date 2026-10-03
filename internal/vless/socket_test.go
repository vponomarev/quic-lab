package vless

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
)

type factoryFunc func(context.Context, string, string) (net.Conn, error)

func (f factoryFunc) DialContext(c context.Context, n, a string) (net.Conn, error) { return f(c, n, a) }
func instanceContext(i *core.Instance) context.Context {
	return context.WithValue(context.Background(), core.XrayKey(1), i)
}
func TestSocketRouterRejectsUnknownInstance(t *testing.T) {
	r := newSocketRouter()
	_, err := r.Dial(context.Background(), nil, xnet.TCPDestination(xnet.LocalHostIP, 443), nil)
	if !errors.Is(err, ErrUnknownEngine) {
		t.Fatalf("got %v", err)
	}
}
func TestSocketRouterSeparatesInstances(t *testing.T) {
	r := newSocketRouter()
	a, b := new(core.Instance), new(core.Instance)
	var mu sync.Mutex
	counts := map[string]int{}
	for name, i := range map[string]*core.Instance{"wifi": a, "cell": b} {
		label := name
		if err := r.register(i, context.Background(), factoryFunc(func(ctx context.Context, n, addr string) (net.Conn, error) {
			mu.Lock()
			counts[label]++
			mu.Unlock()
			return nil, errors.New("bind refused")
		})); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, i := range []*core.Instance{a, b} {
		wg.Add(1)
		go func(i *core.Instance) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, err := r.Dial(instanceContext(i), nil, xnet.TCPDestination(xnet.DomainAddress("outer.invalid"), 443), nil)
				if err == nil {
					t.Error("bind refusal ignored")
				}
			}
		}(i)
	}
	wg.Wait()
	if counts["wifi"] != 10 || counts["cell"] != 10 {
		t.Fatalf("counts %v", counts)
	}
}
func TestSocketRouterUnregisterCancelsAndCloses(t *testing.T) {
	r := newSocketRouter()
	i := new(core.Instance)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	if err := r.register(i, context.Background(), factoryFunc(func(ctx context.Context, n, a string) (net.Conn, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	})); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Dial(instanceContext(i), nil, xnet.TCPDestination(xnet.LocalHostIP, 443), nil)
	}()
	<-started
	r.unregister(i)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("dial not cancelled")
	}
	<-done
	_, err := r.Dial(instanceContext(i), nil, xnet.TCPDestination(xnet.LocalHostIP, 443), nil)
	if !errors.Is(err, ErrUnknownEngine) {
		t.Fatalf("got %v", err)
	}
}
func TestSocketRouterClosesEstablishedAndLateConnections(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "established", true: "late"}[late], func(t *testing.T) {
			r := newSocketRouter()
			i := new(core.Instance)
			client, peer := net.Pipe()
			defer peer.Close()
			entered := make(chan struct{})
			release := make(chan struct{})
			if err := r.register(i, context.Background(), factoryFunc(func(context.Context, string, string) (net.Conn, error) { close(entered); <-release; return client, nil })); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, err := r.Dial(instanceContext(i), nil, xnet.TCPDestination(xnet.LocalHostIP, 443), nil)
				done <- err
			}()
			<-entered
			if late {
				r.unregister(i)
			}
			close(release)
			err := <-done
			if late && err == nil {
				t.Fatal("late connection admitted")
			}
			if !late {
				if err != nil {
					t.Fatal(err)
				}
				r.unregister(i)
			}
			peer.SetReadDeadline(time.Now().Add(time.Second))
			_, err = peer.Read(make([]byte, 1))
			if !errors.Is(err, net.ErrClosed) && err == nil {
				t.Fatal("connection still open")
			}
			if e, ok := err.(net.Error); ok && e.Timeout() {
				t.Fatal("connection leaked")
			}
		})
	}
}
func TestSocketRouterOwnerCancellation(t *testing.T) {
	r := newSocketRouter()
	i := new(core.Instance)
	ctx, cancel := context.WithCancel(context.Background())
	client, peer := net.Pipe()
	defer peer.Close()
	if err := r.register(i, ctx, factoryFunc(func(context.Context, string, string) (net.Conn, error) { return client, nil })); err != nil {
		t.Fatal(err)
	}
	_, err := r.Dial(instanceContext(i), nil, xnet.TCPDestination(xnet.LocalHostIP, 443), nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	peer.SetReadDeadline(time.Now().Add(time.Second))
	_, err = peer.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("connection open")
	}
	if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("owner cancellation leaked connection")
	}
}
func TestSocketRouterFlowCancellationClosesOuter(t *testing.T) {
	r := newSocketRouter()
	i := new(core.Instance)
	client, peer := net.Pipe()
	defer peer.Close()
	if err := r.register(i, context.Background(), factoryFunc(func(context.Context, string, string) (net.Conn, error) { return client, nil })); err != nil {
		t.Fatal(err)
	}
	defer r.unregister(i)
	ctx, cancel := context.WithCancel(instanceContext(i))
	defer cancel()
	if _, err := r.Dial(ctx, nil, xnet.TCPDestination(xnet.LocalHostIP, 443), nil); err != nil {
		t.Fatal(err)
	}
	cancel()
	peer.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	_, err := peer.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("socket remains open")
	}
	if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("flow cancellation leaked outer socket")
	}
}
