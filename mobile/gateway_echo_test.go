package mobile

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"github.com/xtaci/smux"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"quiclab/internal/gateway"
	"testing"
	"time"
)

// Use a live multiplexed VPN tunnel: the gateway accepts the TCP-open request
// but deliberately never replies for stall targets. Cancellation must close
// that stream, without closing the tunnel or delaying a replacement probe.
func pendingEchoGateway(t *testing.T) (*Gateway, *exitEchoSink, <-chan string, <-chan string) {
	t.Helper()
	client, server := net.Pipe()
	cm, err := smux.Client(client, gateway.MuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	sm, err := smux.Server(server, gateway.MuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sink := &exitEchoSink{make(chan map[string]any, 16)}
	g := &Gateway{ctx: ctx, cancel: cancel, mux: cm, sink: sink}
	opened, closed := make(chan string, 16), make(chan string, 16)
	t.Cleanup(func() { g.Stop(); sm.Close() })
	go func() {
		for {
			raw, err := sm.AcceptStream()
			if err != nil {
				return
			}
			go func() {
				st := gateway.NewMStream(raw)
				defer st.Close()
				var req gateway.Request
				if gateway.ReadJSON(st, &req) != nil {
					return
				}
				opened <- req.Address
				if req.Address != "healthy:9000" {
					var b [1]byte
					st.Read(b[:])
					closed <- req.Address
					return
				}
				if gateway.WriteJSON(st, gateway.Reply{}) != nil {
					return
				}
				b := make([]byte, 32)
				if _, err := io.ReadFull(st, b); err == nil {
					st.Write(b)
				}
			}()
		}
	}()
	return g, sink, opened, closed
}
func waitEchoAddress(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("address=%q want=%q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("pending TCP-open did not progress for %s", want)
	}
}
func TestExitEchoCancelsPendingGatewayOpen(t *testing.T) {
	g, sink, opened, closed := pendingEchoGateway(t)
	if err := g.StartExitEcho("stall-first:9000", 60000); err != nil {
		t.Fatal(err)
	}
	waitEchoAddress(t, opened, "stall-first:9000")
	if err := g.StartExitEcho("healthy:9000", 60000); err != nil {
		t.Fatal(err)
	}
	waitEchoAddress(t, closed, "stall-first:9000")
	waitEchoAddress(t, opened, "healthy:9000")
	result := waitEcho(t, sink)
	if result["target"] != "healthy:9000" || result["error"] != nil {
		t.Fatalf("replacement result: %v", result)
	}
	if err := g.StartExitEcho("stall-stop:9000", 60000); err != nil {
		t.Fatal(err)
	}
	waitEchoAddress(t, opened, "stall-stop:9000")
	g.StopExitEcho()
	waitEchoAddress(t, closed, "stall-stop:9000")
	if g.mux.IsClosed() {
		t.Fatal("diagnostic cancellation closed VPN tunnel")
	}
}
func TestExitEchoGatewayOpenHonorsDeadline(t *testing.T) {
	g, _, opened, closed := pendingEchoGateway(t)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		st, err := g.dialStream(ctx, "tcp", "stall-deadline:9000")
		if st != nil {
			st.Close()
		}
		done <- err
	}()
	waitEchoAddress(t, opened, "stall-deadline:9000")
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expired TCP-open succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("gateway TCP-open ignored diagnostic deadline")
	}
	waitEchoAddress(t, closed, "stall-deadline:9000")
	if g.mux.IsClosed() {
		t.Fatal("diagnostic deadline closed VPN tunnel")
	}
}

type exitEchoAPI interface {
	StartExitEcho(string, int64) error
	StopExitEcho()
}
type exitEchoSink struct{ events chan map[string]any }

func (s *exitEchoSink) OnEvent(raw string) {
	var e map[string]any
	if json.Unmarshal([]byte(raw), &e) == nil && e["event"] == "exit_echo" {
		s.events <- e
	}
}
func echoAPI(t *testing.T, g *Gateway) exitEchoAPI {
	t.Helper()
	api, ok := any(g).(exitEchoAPI)
	if !ok {
		t.Fatal("Gateway lacks StartExitEcho/StopExitEcho diagnostic API")
	}
	return api
}
func waitEcho(t *testing.T, s *exitEchoSink) map[string]any {
	t.Helper()
	select {
	case e := <-s.events:
		return e
	case <-time.After(time.Second):
		t.Fatal("no exit_echo result")
		return nil
	}
}

func TestExitEchoCannotFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &exitEchoSink{make(chan map[string]any, 8)}
	g := &Gateway{ctx: ctx, cancel: cancel, sink: sink}
	api := echoAPI(t, g)
	if err := api.StartExitEcho("responder.example:9000", 100); err != nil {
		t.Fatal(err)
	}
	defer api.StopExitEcho()
	e := waitEcho(t, sink)
	if e["error"] == nil || e["rtt_ms"] != nil {
		t.Fatalf("disconnected gateway must emit diagnostic failure: %v", e)
	}
	if g.q != nil || g.mux != nil || g.bond != nil || g.direct != nil {
		t.Fatal("diagnostic changed selected transport")
	}
}

type forbiddenEchoDialer struct{ called bool }

func (d *forbiddenEchoDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	d.called = true
	return nil, errors.New("direct forbidden")
}
func TestExitEchoRejectsDirectGateway(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &forbiddenEchoDialer{}
	g := &Gateway{ctx: ctx, cancel: cancel, direct: d}
	if err := echoAPI(t, g).StartExitEcho("responder.example:9000", 100); err == nil {
		t.Fatal("direct-only gateway accepted VPN diagnostic")
	}
	if d.called {
		t.Fatal("diagnostic dialed directly")
	}
}

func TestExitEchoValidatesConfiguration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := &Gateway{ctx: ctx, cancel: cancel}
	api := echoAPI(t, g)
	for _, target := range []string{"", "https://responder.example", "host:0", "host:65536", "host:abc"} {
		if err := api.StartExitEcho(target, 100); err == nil {
			api.StopExitEcho()
			t.Fatalf("accepted invalid target %q", target)
		}
	}
	for _, interval := range []int64{0, 99, 60001} {
		if err := api.StartExitEcho("host:9000", interval); err == nil {
			api.StopExitEcho()
			t.Fatalf("accepted unbounded interval %d", interval)
		}
	}
}

func TestExitEchoGenerationChangesOnStartReplacementAndStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := &Gateway{ctx: ctx, cancel: cancel}
	defer g.Stop()
	initial := g.ExitEchoGeneration()
	if err := g.StartExitEcho("first:9000", 60000); err != nil {
		t.Fatal(err)
	}
	first := g.ExitEchoGeneration()
	if err := g.StartExitEcho("second:9000", 60000); err != nil {
		t.Fatal(err)
	}
	second := g.ExitEchoGeneration()
	g.StopExitEcho()
	stopped := g.ExitEchoGeneration()
	if first <= initial || second <= first || stopped <= second {
		t.Fatalf("diagnostic generations did not advance: %d %d %d %d", initial, first, second, stopped)
	}
}

func TestExitEchoProbeIntegrityAndCancellation(t *testing.T) {
	for _, mode := range []string{"echo", "corrupt", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			go func() {
				defer server.Close()
				payload := make([]byte, 32)
				if _, err := io.ReadFull(server, payload); err != nil {
					return
				}
				if mode == "cancel" {
					cancel()
					return
				}
				if mode == "corrupt" {
					payload[31] ^= 1
				}
				server.Write(payload)
			}()
			err := exitEchoProbe(ctx, func(context.Context) (net.Conn, error) { return client, nil })
			if (err != nil) != (mode != "echo") {
				t.Fatalf("mode=%s error=%v", mode, err)
			}
		})
	}
}

type reentrantEchoSink struct {
	g       *Gateway
	stopped chan struct{}
}

func (s *reentrantEchoSink) OnEvent(raw string) {
	var e map[string]any
	json.Unmarshal([]byte(raw), &e)
	if e["event"] == "exit_echo" {
		s.g.StopExitEcho()
		select {
		case s.stopped <- struct{}{}:
		default:
		}
	}
}
func TestExitEchoStopFromSink(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &reentrantEchoSink{stopped: make(chan struct{}, 1)}
	g := &Gateway{ctx: ctx, cancel: cancel, sink: s}
	s.g = g
	if err := g.StartExitEcho("host:9000", 100); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.stopped:
	case <-time.After(time.Second):
		t.Fatal("sink StopExitEcho deadlocked")
	}
	g.mu.Lock()
	target := g.exitEchoTarget
	g.mu.Unlock()
	if target != "" {
		t.Fatal("diagnostic remained enabled")
	}
}

func TestExitEchoReplacementAndGatewayStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &exitEchoSink{make(chan map[string]any, 16)}
	g := &Gateway{ctx: ctx, cancel: cancel, sink: sink}
	if err := g.StartExitEcho("first:9000", 60000); err != nil {
		t.Fatal(err)
	}
	first := waitEcho(t, sink)
	if err := g.StartExitEcho("second:9000", 60000); err != nil {
		t.Fatal(err)
	}
	second := waitEcho(t, sink)
	if first["target"] != "first:9000" || second["target"] != "second:9000" || first["echo_generation"] == second["echo_generation"] {
		t.Fatalf("replacement stale: %v %v", first, second)
	}
	g.Stop()
	if err := g.StartExitEcho("third:9000", 100); err == nil {
		t.Fatal("stopped gateway accepted diagnostic")
	}
}

func TestExitEchoDoesNotSwitchPolicyAndCountsBudget(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, []byte(cp), 0600); err != nil {
		t.Fatal(err)
	}
	tc, err := gateway.TLS(pair, ca)
	if err != nil {
		t.Fatal(err)
	}
	tc.NextProtos = []string{"http/1.1"}
	srv, err := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tc)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: http.HandlerFunc(srv.WebSocket)}
	defer hs.Close()
	go hs.Serve(ln)
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		c, err := target.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		b := make([]byte, 32)
		if _, err = io.ReadFull(c, b); err == nil {
			c.Write(b)
		}
	}()
	sink := &exitEchoSink{make(chan map[string]any, 8)}
	g := NewGateway(sink)
	budget, err := NewTrafficBudget("exit-echo", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(gatewayConfig{Transport: "https", Endpoint: ln.Addr().String(), Hostname: "localhost", Certificate: cp, Key: kp, CA: cp})
	if err = g.Start(string(cfg), budget.Bind(nil, "cell")); err != nil {
		t.Fatal(err)
	}
	defer g.Stop()
	// Disable routine RTT so the one diagnostic owns the observed traffic delta.
	g.SetRTT(false, 5000)
	before := budget.ledger.Snapshot().Used
	if err = g.StartExitEcho(target.Addr().String(), 60000); err != nil {
		t.Fatal(err)
	}
	e := waitEcho(t, sink)
	g.StopExitEcho()
	if e["error"] != nil || e["rtt_ms"] == nil {
		t.Fatalf("VPN echo failed: %v", e)
	}
	if after := budget.ledger.Snapshot().Used; after <= before {
		t.Fatalf("diagnostic physical bytes not charged: %d -> %d", before, after)
	}
	target.Close()
	if err = g.StartExitEcho(target.Addr().String(), 60000); err != nil {
		t.Fatal(err)
	}
	failed := waitEcho(t, sink)
	g.StopExitEcho()
	if failed["error"] == nil {
		t.Fatalf("closed responder did not fail: %v", failed)
	}
	if g.cfg.Transport != "https" || g.bond != nil || g.direct != nil || g.probes.enabledNow() {
		t.Fatal("diagnostic failure changed transport or RTT policy")
	}
}
