package bond

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type fakePath struct {
	ctx     context.Context
	cancel  context.CancelFunc
	in, out chan []byte
	drop    *atomic.Bool
	delay   time.Duration
}

func (p *fakePath) SendDatagram(b []byte) error {
	if p.ctx.Err() != nil {
		return net.ErrClosed
	}
	if p.drop.Load() {
		return nil
	}
	b = append([]byte(nil), b...)
	go func() {
		time.Sleep(p.delay)
		select {
		case p.out <- b:
		case <-p.ctx.Done():
		}
	}()
	return nil
}
func (p *fakePath) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	select {
	case b := <-p.in:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.ctx.Done():
		return nil, net.ErrClosed
	}
}
func (p *fakePath) Close() error { p.cancel(); return nil }
func pair(delay time.Duration) (*fakePath, *fakePath, *atomic.Bool) {
	ctx, cancel := context.WithCancel(context.Background())
	a, b := make(chan []byte, 8192), make(chan []byte, 8192)
	drop := &atomic.Bool{}
	return &fakePath{ctx, cancel, a, b, drop, delay}, &fakePath{ctx, cancel, b, a, drop, delay}, drop
}
func setup(t *testing.T) (*Mux, *Mux, *atomic.Bool) {
	t.Helper()
	a, b := NewMux(context.Background(), true), NewMux(context.Background(), false)
	t.Cleanup(func() { a.Close(); b.Close() })
	wa, wb, drop := pair(5 * time.Millisecond)
	a.Session.AddPath("wifi", wa)
	b.Session.AddPath("wifi", wb)
	ca, cb, _ := pair(12 * time.Millisecond)
	a.Session.AddPath("cell", ca)
	b.Session.AddPath("cell", cb)
	return a, b, drop
}
func TestStreamSurvivesBlackholeAndNoDuplicateBytes(t *testing.T) {
	a, b, drop := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	client, e := a.OpenStream(ctx)
	if e != nil {
		t.Fatal(e)
	}
	server, e := b.AcceptStream(ctx)
	if e != nil {
		t.Fatal(e)
	}
	client.SetDeadline(time.Now().Add(7 * time.Second))
	server.SetDeadline(time.Now().Add(7 * time.Second))
	go func() { io.Copy(server, server); server.CloseWrite() }()
	payload := bytes.Repeat([]byte("unique payload\x00"), 40000)
	done := make(chan error, 1)
	go func() {
		_, e := client.Write(payload)
		if e == nil {
			e = client.CloseWrite()
		}
		done <- e
	}()
	time.AfterFunc(40*time.Millisecond, func() { drop.Store(true) })
	got, e := io.ReadAll(client)
	if e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(payload, got) {
		t.Fatalf("corrupt stream: got %d want %d", len(got), len(payload))
	}
	if a.Session.Stats().Rescued+b.Session.Stats().Rescued == 0 {
		t.Fatal("reserve never rescued records")
	}
}
func TestSlowFlowDoesNotBlockAnother(t *testing.T) {
	a, b, _ := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	slow, _ := a.OpenStream(ctx)
	_, e := b.AcceptStream(ctx)
	if e != nil {
		t.Fatal(e)
	}
	go slow.Write(make([]byte, 1<<20))
	fast, _ := a.OpenStream(ctx)
	peer, e := b.AcceptStream(ctx)
	if e != nil {
		t.Fatal(e)
	}
	fast.SetDeadline(time.Now().Add(time.Second))
	peer.SetDeadline(time.Now().Add(time.Second))
	go peer.Write([]byte("ok"))
	buf := make([]byte, 2)
	if _, e = io.ReadFull(fast, buf); e != nil || string(buf) != "ok" {
		t.Fatalf("independent flow blocked: %q %v", buf, e)
	}
}
func TestUDPExpiresAndDoesNotBlockFollowingDatagram(t *testing.T) {
	a, b, drop := setup(t)
	drop.Store(true)
	a.Session.RemovePath("cell")
	b.Session.RemovePath("cell")
	a.SendDatagram([]byte("stale"))
	time.Sleep(300 * time.Millisecond)
	if a.Session.Stats().Expired == 0 {
		t.Fatal("missing expiration")
	}
	drop.Store(false)
	a.SendDatagram([]byte("fresh"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, e := b.ReceiveDatagram(ctx)
	if e != nil || string(got) != "fresh" {
		t.Fatalf("%q %v", got, e)
	}
}
func TestUDPRemovesDelayedDuplicate(t *testing.T) {
	a, b, _ := setup(t)
	w1, w2, _ := pair(100 * time.Millisecond)
	a.Session.AddPath("wifi", w1)
	b.Session.AddPath("wifi", w2)
	a.Session.mu.Lock()
	a.Session.paths["wifi"].healthySince = time.Now().Add(-10 * time.Second)
	a.Session.paths["wifi"].rtt = 10 * time.Millisecond
	a.Session.paths["wifi"].variance = 0
	a.Session.mu.Unlock()
	a.SendDatagram([]byte("one"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, e := b.ReceiveDatagram(ctx)
	if e != nil || string(got) != "one" {
		t.Fatal(e)
	}
	time.Sleep(200 * time.Millisecond)
	select {
	case <-b.datagrams:
		t.Fatal("duplicate delivered")
	default:
	}
	if b.Session.Stats().Duplicates == 0 {
		t.Fatal("duplicate not observed")
	}
}
func TestPathReplacementKeepsSession(t *testing.T) {
	a, b, _ := setup(t)
	a.Session.RemovePath("wifi")
	b.Session.RemovePath("wifi")
	x, y, _ := pair(time.Millisecond)
	a.Session.AddPath("wifi", x)
	b.Session.AddPath("wifi", y)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f, _ := a.OpenStream(ctx)
	peer, e := b.AcceptStream(ctx)
	if e != nil {
		t.Fatal(e)
	}
	f.SetDeadline(time.Now().Add(time.Second))
	peer.SetDeadline(time.Now().Add(time.Second))
	peer.Write([]byte("x"))
	v := make([]byte, 1)
	if _, e = f.Read(v); e != nil || v[0] != 'x' {
		t.Fatal(e)
	}
}

func TestCopyBudgetAndCellQuota(t *testing.T) {
	a, _, drop := setup(t)
	a.Session.Configure(0, 150)
	drop.Store(true)
	a.SendDatagram([]byte("do not duplicate"))
	time.Sleep(250 * time.Millisecond)
	for _, p := range a.Session.Stats().Paths {
		if p.Name == "cell" && p.Copies != 0 {
			t.Fatal("speculative copy exceeded disabled budget")
		}
	}
	a.Session.RemovePath("wifi")
	a.SendDatagram(make([]byte, 200))
	time.Sleep(50 * time.Millisecond)
	if a.Session.CellAllowed() {
		t.Fatal("LTE quota ignored")
	}
	if a.Session.Stats().CellSent > 150 {
		t.Fatal("accounted quota exceeded")
	}
	x, y, _ := pair(time.Millisecond)
	defer x.Close()
	defer y.Close()
	if a.Session.AddPath("cell", x) == nil {
		t.Fatal("reconnect reset LTE quota")
	}
}
func TestSlowReaderTimeoutDoesNotKillSession(t *testing.T) {
	a, b, _ := setup(t)
	a.Session.mu.Lock()
	a.Session.recordTimeout = 150 * time.Millisecond
	a.Session.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	slow, _ := a.OpenStream(ctx)
	if _, e := b.AcceptStream(ctx); e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() { slow.Write(make([]byte, 1<<20)); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked writer not released")
	}
	if a.Context().Err() != nil {
		t.Fatal("one slow reader killed whole session")
	}
	fast, e := a.OpenStream(ctx)
	if e != nil {
		t.Fatal(e)
	}
	peer, e := b.AcceptStream(ctx)
	if e != nil {
		t.Fatal(e)
	}
	fast.SetDeadline(time.Now().Add(time.Second))
	peer.Write([]byte("ok"))
	v := make([]byte, 2)
	if _, e = io.ReadFull(fast, v); e != nil || string(v) != "ok" {
		t.Fatal(e)
	}
}
func TestClosedFlowCannotBeReopened(t *testing.T) {
	m := NewMux(context.Background(), false)
	defer m.Close()
	if !m.deliver(Record{Kind: Open, Flow: 1}) {
		t.Fatal("open")
	}
	f := <-m.accepted
	f.Close()
	if !m.deliver(Record{Kind: Open, Flow: 1}) {
		t.Fatal("duplicate must be acknowledged")
	}
	select {
	case <-m.accepted:
		t.Fatal("late OPEN recreated flow")
	default:
	}
}

func TestCloseDeliversReplyAndStopsPeerWrites(t *testing.T) {
	a, b, _ := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client, _ := a.OpenStream(ctx)
	server, e := b.AcceptStream(ctx)
	if e != nil {
		t.Fatal(e)
	}
	client.SetDeadline(time.Now().Add(time.Second))
	server.Write([]byte("reply"))
	server.Close()
	data, e := io.ReadAll(client)
	if e != nil || string(data) != "reply" {
		t.Fatalf("close lost queued reply: %q %v", data, e)
	}
	time.Sleep(30 * time.Millisecond)
	if _, e = client.Write([]byte("do not download forever")); e == nil {
		t.Fatal("closed reader did not stop peer writes")
	}
}
