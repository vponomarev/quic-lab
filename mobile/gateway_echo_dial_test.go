package mobile

import (
	"context"
	"errors"
	"io"
	"quiclab/internal/gateway"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockedDiagnosticClose struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func newBlockedDiagnosticClose() *blockedDiagnosticClose {
	return &blockedDiagnosticClose{started: make(chan struct{}), release: make(chan struct{})}
}
func (s *blockedDiagnosticClose) Read([]byte) (int, error)    { return 0, io.EOF }
func (s *blockedDiagnosticClose) Write(b []byte) (int, error) { return len(b), nil }
func (s *blockedDiagnosticClose) SetDeadline(time.Time) error { return nil }
func (s *blockedDiagnosticClose) CloseWrite() error           { return nil }
func (s *blockedDiagnosticClose) Close() error {
	first := false
	s.once.Do(func() { first = true; s.calls.Add(1); close(s.started) })
	if first {
		<-s.release
	}
	return nil
}
func releaseDiagnostic(s *blockedDiagnosticClose) func() {
	var once sync.Once
	return func() { once.Do(func() { close(s.release) }) }
}

func TestExitEchoDiagnosticCloseIsPromptAndIsolated(t *testing.T) {
	g, _, _, _ := pendingEchoGateway(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	raw := newBlockedDiagnosticClose()
	release := releaseDiagnostic(raw)
	defer release()
	st, err := g.openExitEchoDiagnostic(ctx, func(context.Context) (gateway.Stream, error) { return raw, nil })
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() { st.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("diagnostic caller waited for blocked FIN cleanup")
	}
	select {
	case <-raw.started:
	case <-time.After(time.Second):
		t.Fatal("diagnostic worker did not own cleanup")
	}
	st.Close()
	cancel()
	if raw.calls.Load() != 1 {
		t.Fatal("underlying diagnostic closed more than once")
	}
	if g.mux.IsClosed() {
		t.Fatal("diagnostic cleanup closed shared VPN mux")
	}
	appCtx, appCancel := context.WithTimeout(context.Background(), time.Second)
	defer appCancel()
	app, err := g.dialStream(appCtx, "tcp", "healthy:9000")
	if err != nil {
		t.Fatalf("unrelated stream blocked by diagnostic cleanup: %v", err)
	}
	defer app.Close()
	payload := make([]byte, 32)
	if _, err = app.Write(payload); err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadFull(app, payload); err != nil {
		t.Fatalf("unrelated VPN stream no longer exchanges data: %v", err)
	}
}

func TestExitEchoDiagnosticCanceledDialKeepsOneWorkerAndRecovers(t *testing.T) {
	g := &Gateway{}
	raw := newBlockedDiagnosticClose()
	releaseClose := releaseDiagnostic(raw)
	defer releaseClose()
	releaseDial := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseDial) }) }
	defer release()
	entered := make(chan struct{})
	var opens atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := g.openExitEchoDiagnostic(ctx, func(context.Context) (gateway.Stream, error) {
			opens.Add(1)
			close(entered)
			<-releaseDial
			return raw, nil
		})
		done <- err
	}()
	<-entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("diagnostic caller waited for abandoned dial/failed Close")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel2()
	_, err := g.openExitEchoDiagnostic(ctx2, func(context.Context) (gateway.Stream, error) { opens.Add(1); return raw, nil })
	if !errors.Is(err, context.DeadlineExceeded) || opens.Load() != 1 {
		t.Fatalf("pending diagnostic spawned another worker: opens=%d error=%v", opens.Load(), err)
	}
	release()
	select {
	case <-raw.started:
	case <-time.After(time.Second):
		t.Fatal("abandoned stream was not cleaned up")
	}
	ctx3, cancel3 := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel3()
	_, err = g.openExitEchoDiagnostic(ctx3, func(context.Context) (gateway.Stream, error) { opens.Add(1); return raw, nil })
	if !errors.Is(err, context.DeadlineExceeded) || opens.Load() != 1 {
		t.Fatalf("cleanup released gate too early: opens=%d error=%v", opens.Load(), err)
	}
	releaseClose()
	recovered := newBlockedDiagnosticClose()
	releaseRecovered := releaseDiagnostic(recovered)
	releaseRecovered()
	ctx4, cancel4 := context.WithTimeout(context.Background(), time.Second)
	defer cancel4()
	st, err := g.openExitEchoDiagnostic(ctx4, func(context.Context) (gateway.Stream, error) { opens.Add(1); return recovered, nil })
	if err != nil {
		t.Fatalf("diagnostics did not recover after cleanup: %v", err)
	}
	st.Close()
	select {
	case <-recovered.started:
	case <-time.After(time.Second):
		t.Fatal("recovered stream was not closed")
	}
}
