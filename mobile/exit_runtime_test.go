package mobile

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/quic-go/quic-go"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"quiclab/internal/bond"
	"quiclab/internal/gateway"
	"quiclab/internal/pathpolicy"
	"quiclab/internal/protocol"
	"quiclab/internal/trafficbudget"
	"quiclab/internal/vpnmodel"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Only the socket boundary is substituted; admission, shared budget, generations,
// cancellation, health and pool accounting are the real runtime.
type runtimeTransport struct {
	mu           sync.Mutex
	paths        map[string]bond.PathStats
	dials        []bond.PathInfo
	fail         map[string]bool
	entered      chan struct{}
	hold         bool
	closed       bool
	checks       int
	checkGate    <-chan struct{}
	checkEntered chan struct{}
}

func (b *runtimeTransport) Dial(ctx context.Context, cfg gatewayConfig, info bond.PathInfo, binder SocketBinder) error {
	b.mu.Lock()
	b.dials = append(b.dials, info)
	fail := b.fail[info.ProfileID]
	hold := b.hold
	entered := b.entered
	b.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if hold {
		<-ctx.Done()
		return ctx.Err()
	}
	if fail {
		return errors.New("unavailable transport")
	}
	if binder != nil {
		if err := binder.Bind(0); err != nil {
			return err
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || ctx.Err() != nil {
		return context.Canceled
	}
	if b.paths == nil {
		b.paths = map[string]bond.PathStats{}
	}
	b.paths[info.ID] = bond.PathStats{Name: info.ID, ProfileID: info.ProfileID, Network: info.Network, Generation: info.Generation, Ready: true, DataProbes: 1, RTTMS: 1}
	return nil
}
func (b *runtimeTransport) Remove(info bond.PathInfo) {
	b.mu.Lock()
	delete(b.paths, info.ID)
	b.mu.Unlock()
}
func (b *runtimeTransport) Probe(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.paths[id]
	p.DataProbes++
	b.paths[id] = p
	return nil
}
func (b *runtimeTransport) Stats() bond.Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := bond.Stats{}
	for _, p := range b.paths {
		s.Paths = append(s.Paths, p)
	}
	return s
}
func (b *runtimeTransport) Close() { b.mu.Lock(); b.closed = true; b.paths = nil; b.mu.Unlock() }
func (b *runtimeTransport) counts() (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cell := 0
	for _, p := range b.dials {
		if p.Network == "cell" {
			cell++
		}
	}
	return len(b.dials), cell
}
func runtimeConfig(economy bool, extra string) string {
	return `{"model":{"version":1,"exits":[{"id":"exit","name":"Test","kind":"demux","demux_id":"gateway"}],"profiles":[{"id":"auto","exit_id":"exit","transport":"quic","mode":"auto","endpoint":"127.0.0.1:4443","pool_size":3}` + extra + `]},"gateways":{"auto":{"transport":"quic","endpoint":"127.0.0.1:4443"},"reserve":{"transport":"https","endpoint":"127.0.0.1:4444"}},"initial_network":{"network":"wifi","available":true,"allowed":true,"generation":1},"economy":` + map[bool]string{true: "true", false: "false"}[economy] + `}`
}
func awaitRuntime(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("runtime condition did not become true")
}
func startTestRuntime(t *testing.T, b *runtimeTransport, economy bool, extra string) (*ExitRuntime, *TrafficBudget) {
	t.Helper()
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("test-run", 10000)
	if err := r.SetTrafficBudget(budget); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(runtimeConfig(economy, extra), nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Stop)
	return r, budget
}

func TestRuntimePoolAcrossNetworksAndEconomy(t *testing.T) {
	for _, economy := range []bool{true, false} {
		t.Run(map[bool]string{true: "economy", false: "availability"}[economy], func(t *testing.T) {
			b := &runtimeTransport{}
			r, _ := startTestRuntime(t, b, economy, "")
			if err := r.UpdateNetwork(`{"network":"cell","available":true,"allowed":true,"generation":1}`, nil); err != nil {
				t.Fatal(err)
			}
			awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 3 })
			time.Sleep(150 * time.Millisecond)
			_, cell := b.counts()
			if economy && cell != 0 {
				t.Fatal("economy warmed LTE while Wi-Fi works")
			}
			if !economy && cell == 0 {
				t.Fatal("availability failed to prepare LTE")
			}
			if len(b.Stats().Paths) > 3 {
				t.Fatal("pool limit multiplied by network")
			}
			if err := r.UpdateNetwork(`{"network":"wifi","available":false,"allowed":true,"generation":2}`, nil); err != nil {
				t.Fatal(err)
			}
			awaitRuntime(t, func() bool {
				ps := b.Stats().Paths
				if len(ps) != 3 {
					return false
				}
				for _, p := range ps {
					if p.Network != "cell" {
						return false
					}
				}
				return true
			})
		})
	}
}
func TestStoppedGenerationCannotReplenishPool(t *testing.T) {
	b := &runtimeTransport{hold: true, entered: make(chan struct{}, 1)}
	r, _ := startTestRuntime(t, b, true, "")
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		t.Fatal("dial not started")
	}
	done := make(chan struct{})
	go func() { r.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel dialing generation")
	}
	if err := r.UpdateNetwork(`{"network":"wifi","available":true,"allowed":true,"generation":2}`, nil); err == nil {
		t.Fatal("stopped runtime accepted network callback")
	}
	n, _ := b.counts()
	time.Sleep(150 * time.Millisecond)
	after, _ := b.counts()
	if n != after || len(b.Stats().Paths) != 0 {
		t.Fatal("stopped generation replenished pool")
	}
}
func TestReconnectDoesNotResetCellBudget(t *testing.T) {
	b := &runtimeTransport{}
	r, budget := startTestRuntime(t, b, true, "")
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) > 0 })
	id, ok := budget.meter.Reserve(10000, trafficbudget.User)
	if !ok {
		t.Fatal("reservation")
	}
	budget.meter.Commit(id, 10000)
	if err := r.UpdateNetwork(`{"network":"cell","available":true,"allowed":true,"generation":1}`, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateNetwork(`{"network":"wifi","available":false,"allowed":true,"generation":2}`, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	_, cell := b.counts()
	if cell != 0 || budget.CellAllowed() {
		t.Fatal("network reconnect bypassed exhausted shared budget")
	}
	r.Stop()
	next := NewExitRuntime(nil)
	next.backend = &runtimeTransport{}
	if err := next.SetTrafficBudget(budget); err != nil {
		t.Fatal(err)
	}
	if err := next.Start(runtimeConfig(false, ""), nil); err != nil {
		t.Fatal(err)
	}
	defer next.Stop()
	if err := next.UpdateNetwork(`{"network":"cell","available":true,"allowed":true,"generation":1}`, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	_, cell = next.backend.(*runtimeTransport).counts()
	if cell != 0 {
		t.Fatal("exit restart reset run budget")
	}
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(next.Snapshot()), &snapshot); err != nil {
		t.Fatal(err)
	}
}
func TestReserveChecksDoNotStartCarousel(t *testing.T) {
	extra := `,{"id":"reserve","exit_id":"exit","transport":"https","mode":"reserve","endpoint":"127.0.0.1:4444","priority":1,"pool_size":5,"check_reserve":false}`
	b := &runtimeTransport{}
	_, _ = startTestRuntime(t, b, true, extra)
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 3 })
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range b.dials {
		if p.ProfileID == "reserve" {
			t.Fatal("reserve dialed before automatic paths failed")
		}
	}
}

func TestOptInReserveProbeDoesNotJoinUserSession(t *testing.T) {
	extra := `,{"id":"reserve","exit_id":"exit","transport":"https","mode":"reserve","endpoint":"127.0.0.1:4444","priority":1,"pool_size":5,"check_reserve":true}`
	b := &runtimeTransport{}
	r, _ := startTestRuntime(t, b, true, extra)
	awaitRuntime(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.checks > 0 })
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.checks != 1 {
		t.Fatal("reserve check repeated without cadence")
	}
	for _, p := range b.dials {
		if p.ProfileID == "reserve" {
			t.Fatal("reserve check joined user session or carousel")
		}
	}
	_ = r
}
func (b *runtimeTransport) Check(ctx context.Context, cfg gatewayConfig, info bond.PathInfo, binder SocketBinder) error {
	b.mu.Lock()
	b.checks++
	gate, entered := b.checkGate, b.checkEntered
	b.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if gate != nil {
		<-gate
	} // Deliberately model a transport reporting late success after cancellation.
	return nil
}

func TestReserveWaitsForEveryAutomaticNetwork(t *testing.T) {
	extra := `,{"id":"reserve","exit_id":"exit","transport":"https","mode":"reserve","endpoint":"127.0.0.1:4444","priority":1,"pool_size":2}`
	b := &runtimeTransport{fail: map[string]bool{"auto": true}}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("ordering", 10000)
	if err := r.SetTrafficBudget(budget); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(runtimeConfig(false, extra), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	if err := r.UpdateNetwork(`{"network":"cell","available":true,"allowed":true,"generation":1}`, nil); err != nil {
		t.Fatal(err)
	}
	awaitRuntime(t, func() bool {
		for _, p := range b.Stats().Paths {
			if p.ProfileID == "reserve" {
				return true
			}
		}
		return false
	})
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := map[string]bool{}
	for _, p := range b.dials {
		if p.ProfileID == "auto" {
			seen[p.Network] = true
		}
		if p.ProfileID == "reserve" {
			if !seen["wifi"] || !seen["cell"] {
				t.Fatal("reserve bypassed automatic network", b.dials)
			}
			break
		}
	}
}

func TestRuntimeQUICHTTPSKeepsTCPAndUDPSession(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, []byte(cp), 0600); err != nil {
		t.Fatal(err)
	}
	tc, err := gateway.TLS(pair, ca)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.BondContext = ctx
	q, err := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{EnableDatagrams: true})
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	go srv.ServeQUIC(ctx, q)
	ht := tc.Clone()
	ht.NextProtos = []string{"http/1.1"}
	ln, err := tls.Listen("tcp4", "127.0.0.1:0", ht)
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: http.HandlerFunc(srv.ServeBondHTTPS)}
	defer hs.Close()
	go hs.Serve(ln)
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	var accepted atomic.Int32
	go func() {
		for {
			c, err := target.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	sources := make(chan string, 8)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			sources <- from.String()
			udp.WriteTo(buf[:n], from)
		}
	}()
	model := vpnmodel.Config{Version: 1, Exits: []vpnmodel.Exit{{ID: "exit", Name: "test", Kind: "demux", DemuxID: "same-registry"}}, Profiles: []vpnmodel.Profile{
		{ID: "quic", ExitID: "exit", Transport: "quic", Mode: "auto", Endpoint: q.Addr().String(), PoolSize: 2},
		{ID: "https", ExitID: "exit", Transport: "https", Mode: "reserve", Endpoint: ln.Addr().String(), PoolSize: 2, Priority: 1},
	}}
	configs := map[string]gatewayConfig{}
	for _, p := range model.Profiles {
		configs[p.ID] = gatewayConfig{Transport: p.Transport, Endpoint: p.Endpoint, Hostname: "localhost", Certificate: cp, Key: kp, CA: cp, BondCopyBudget: 1 << 20}
	}
	raw, _ := json.Marshal(exitRuntimeConfig{Model: model, Gateways: configs, InitialNetwork: runtimeNetwork{Network: "wifi", Allowed: true, Available: true, Generation: 1}, Economy: true})
	r := NewExitRuntime(nil)
	budget, _ := NewTrafficBudget("mixed-runtime", 1<<20)
	if err = r.SetTrafficBudget(budget); err != nil {
		t.Fatal(err)
	}
	if err = r.Start(string(raw), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.preferred != "" && len(r.paths) == 2 })
	g := r.Gateway()
	g.mu.Lock()
	token := g.bondToken
	g.mu.Unlock()
	stream, err := g.dialStream(ctx, "tcp", target.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(12 * time.Second))
	mapping, err := (bondUDP{g}).DialContext(ctx, "udp4", udp.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer mapping.Close()
	mapping.SetDeadline(time.Now().Add(12 * time.Second))
	exchange := func(label string) {
		t.Helper()
		payload := bytes.Repeat([]byte(label), 400)
		if _, err := stream.Write(payload); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, len(payload))
		if _, err := io.ReadFull(stream, got); err != nil || !bytes.Equal(got, payload) {
			t.Fatal("TCP bytes changed across transport", err)
		}
		if _, err := mapping.Write([]byte(label)); err != nil {
			t.Fatal(err)
		}
		got = make([]byte, len(label))
		if _, err := mapping.Read(got); err != nil || string(got) != label {
			t.Fatal("UDP echo", err)
		}
	}
	exchange("before")
	source := <-sources
	// Retire every live QUIC transport and refuse its reconnects. The HTTPS profile
	// must join the retained gateway token while the application sockets stay open.
	q.Close()
	for _, p := range r.backend.Stats().Paths {
		r.backend.Remove(bond.PathInfo{ID: p.Name, ProfileID: p.ProfileID, Network: p.Network, Generation: p.Generation})
	}
	deadline := time.Now().Add(8 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		for _, p := range r.backend.Stats().Paths {
			if p.ProfileID == "https" && p.Ready {
				ready = true
			}
		}
		if ready {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("HTTPS fallback did not recover", r.Snapshot())
	}
	exchange("after")
	if got := <-sources; got != source {
		t.Fatalf("UDP mapping changed %s -> %s", source, got)
	}
	g.mu.Lock()
	same := token == g.bondToken
	g.mu.Unlock()
	if !same || r.Gateway() != g || accepted.Load() != 1 {
		t.Fatal("transport switch replaced logical session or TCP exit socket")
	}
}

func TestRuntimeFailureBackoffReachesThirtySeconds(t *testing.T) {
	r := NewExitRuntime(nil)
	c := &runtimeCandidate{}
	now := time.Unix(100, 0)
	for i := 0; i < 6; i++ {
		r.fail(c, now)
	}
	delay := c.retry.Sub(now)
	if delay < 24*time.Second || delay > 36*time.Second {
		t.Fatalf("six failures must reach 30s jittered backoff, got %s", delay)
	}
}

func TestRuntimeUsefulProgressResetsBackoff(t *testing.T) {
	b := &runtimeTransport{}
	r, _ := startTestRuntime(t, b, true, "")
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 3 })
	r.mu.Lock()
	defer r.mu.Unlock()
	var path *runtimePath
	for _, p := range r.paths {
		path = p
		break
	}
	c := r.candidate(path)
	c.failures = 6
	b.mu.Lock()
	s := b.paths[path.info.ID]
	s.AckedBytes = 100
	b.paths[path.info.ID] = s
	b.mu.Unlock()
	now := time.Now()
	r.refresh(now)
	for sec := 1; sec <= 31; sec++ {
		b.mu.Lock()
		s = b.paths[path.info.ID]
		s.AckedBytes += 100
		b.paths[path.info.ID] = s
		b.mu.Unlock()
		r.refresh(now.Add(time.Duration(sec) * time.Second))
	}
	if c.failures != 0 {
		t.Fatal("sustained useful data did not reset retry backoff")
	}
}

func TestRuntimeRTTDisabledUsesRareHealthProbes(t *testing.T) {
	b := &runtimeTransport{}
	r, _ := startTestRuntime(t, b, true, "")
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 3 })
	r.SetRTT(false, 1000)
	before := uint64(0)
	for _, p := range b.Stats().Paths {
		before += p.DataProbes
	}
	time.Sleep(1300 * time.Millisecond)
	after := uint64(0)
	for _, p := range b.Stats().Paths {
		after += p.DataProbes
	}
	if after != before {
		t.Fatal("disabled RTT kept frequent diagnostic probes")
	}
}

func TestRuntimeCapabilitiesRespectCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := NewGateway(nil)
	g.ctx = ctx
	g.cfg.ControlURL = "https://127.0.0.1:1"
	if err := g.checkCapabilities(nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("capability request escaped canceled runtime: %v", err)
	}
}

func policyRuntimeFixture(t *testing.T, profiles []vpnmodel.Profile) *ExitRuntime {
	t.Helper()
	budget, _ := NewTrafficBudget("policy-fixture", 0)
	b := &runtimeTransport{paths: map[string]bond.PathStats{}}
	r := NewExitRuntime(nil)
	r.backend = b
	r.budget = budget
	r.cfg.Model = vpnmodel.Config{Exits: []vpnmodel.Exit{{ID: "exit"}}, Profiles: profiles}
	r.paths = map[string]*runtimePath{}
	r.pools = map[string]*pathpolicy.Pool{}
	r.serial = map[string]uint64{}
	r.networks = map[string]runtimeNetwork{"wifi": {Network: "wifi", Available: true, Allowed: true, Generation: 1}}
	r.probeInterval = time.Second
	for _, p := range profiles {
		r.pools[p.ID], _ = pathpolicy.NewPool(p.PoolSize)
		r.candidates = append(r.candidates, &runtimeCandidate{profile: p, network: "wifi", attempted: true})
	}
	return r
}
func addPolicyPath(t *testing.T, r *ExitRuntime, profile string, generation uint64, now time.Time) *runtimePath {
	t.Helper()
	reservation, err := r.pools[profile].Reserve(profile, "wifi")
	if err != nil {
		t.Fatal(err)
	}
	p := &runtimePath{info: bond.PathInfo{ID: profile + "/0", ProfileID: profile, Network: "wifi", Generation: generation}, reservation: reservation, networkGeneration: 1, healthySince: now.Add(-9 * time.Second), successes: 3, ready: true, nextProbe: now.Add(time.Minute)}
	r.paths[p.info.ID] = p
	b := r.backend.(*runtimeTransport)
	b.paths[p.info.ID] = bond.PathStats{Name: p.info.ID, ProfileID: profile, Network: "wifi", Generation: generation, Ready: true, DataProbes: 1, AckedBytes: 100, RTTMS: 1}
	return p
}
func TestAutoRecoveryRetiresReserveUserPaths(t *testing.T) {
	r := policyRuntimeFixture(t, []vpnmodel.Profile{{ID: "auto", Mode: "auto", PoolSize: 2}, {ID: "reserve", Mode: "reserve", PoolSize: 2, Priority: 1}})
	now := time.Now()
	auto := addPolicyPath(t, r, "auto", 1, now)
	reserve := addPolicyPath(t, r, "reserve", 1, now)
	r.preferred = reserve.info.ID
	r.refresh(now)
	if r.preferred != auto.info.ID {
		t.Fatal("stable automatic path was not restored")
	}
	for _, p := range r.backend.Stats().Paths {
		if p.ProfileID == "reserve" {
			t.Fatal("reserve still available for user traffic after automatic recovery")
		}
	}
}
func TestAutomaticProfileOutranksReservePriority(t *testing.T) {
	r := policyRuntimeFixture(t, []vpnmodel.Profile{{ID: "reserve", Mode: "reserve", PoolSize: 2, Priority: 0}, {ID: "auto", Mode: "auto", PoolSize: 2, Priority: 10}})
	now := time.Now()
	reserve := addPolicyPath(t, r, "reserve", 1, now)
	auto := addPolicyPath(t, r, "auto", 1, now)
	r.preferred = reserve.info.ID
	r.refresh(now)
	if r.preferred != auto.info.ID {
		t.Fatal("reserve priority bypassed automatic profile group")
	}
}
func TestRepeatedStallRecoveryRecommendsCarousel(t *testing.T) {
	r := policyRuntimeFixture(t, []vpnmodel.Profile{{ID: "auto", Mode: "auto", PoolSize: 1}})
	now := time.Now()
	p := addPolicyPath(t, r, "auto", 1, now)
	r.preferred = p.info.ID
	r.refresh(now)
	for i := 0; i < 3; i++ {
		at := now.Add(time.Duration(i*3+1) * time.Second)
		b := r.backend.(*runtimeTransport)
		s := b.paths[p.info.ID]
		s.PendingBytes = 900
		b.paths[p.info.ID] = s
		r.refresh(at)
		r.refresh(at.Add(time.Second))
		if len(r.paths) != 0 {
			t.Fatal("fixture did not detect data stall")
		}
		p = addPolicyPath(t, r, "auto", uint64(i+2), at.Add(2*time.Second))
		r.refresh(at.Add(2 * time.Second))
	}
	recommendations := 0
	for _, event := range r.events {
		if event["event"] == "recommend_carousel" {
			recommendations++
		}
	}
	if recommendations != 1 {
		t.Fatalf("three useful recoveries should recommend carousel once, got %d", recommendations)
	}
}
func TestRuntimeLongProfileIDHasBoundedWireIdentity(t *testing.T) {
	var cfg exitRuntimeConfig
	if err := json.Unmarshal([]byte(runtimeConfig(true, "")), &cfg); err != nil {
		t.Fatal(err)
	}
	longID := strings.Repeat("p", 128)
	cfg.Model.Profiles[0].ID = longID
	cfg.Gateways[longID] = cfg.Gateways["auto"]
	delete(cfg.Gateways, "auto")
	raw, _ := json.Marshal(cfg)
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("long-profile", 0)
	if err := r.SetTrafficBudget(budget); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(string(raw), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 3 })
	for _, p := range b.Stats().Paths {
		if len(p.Name) > 128 {
			t.Fatalf("valid model creates invalid wire path ID of %d bytes", len(p.Name))
		}
		if p.ProfileID != longID {
			t.Fatal("public profile identity changed")
		}
	}
}

type runtimeEvents chan map[string]any

func (s runtimeEvents) OnEvent(raw string) {
	var v map[string]any
	if json.Unmarshal([]byte(raw), &v) == nil {
		select {
		case s <- v:
		default:
		}
	}
}
func TestRuntimeEventsKeepBondDiagnosticsAndHealth(t *testing.T) {
	sink := make(runtimeEvents, 128)
	b := &runtimeTransport{}
	r := NewExitRuntime(sink)
	r.backend = b
	budget, _ := NewTrafficBudget("events", 0)
	if err := r.SetTrafficBudget(budget); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(runtimeConfig(true, ""), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	stats, echo := false, false
	deadline := time.After(2 * time.Second)
	for !stats || !echo {
		select {
		case e := <-sink:
			switch e["event"] {
			case "bond_stats":
				stats = true
			case "echo":
				if e["connection_id"] != "" && e["connection_id"] != nil {
					echo = true
				}
			}
		case <-deadline:
			t.Fatal("runtime omitted bond stats or stable logical connection ID")
		}
	}
	r.SetRTT(false, 1000)
	for _, p := range b.Stats().Paths {
		b.Probe(p.Name)
	}
	deadline = time.After(time.Second)
	for {
		select {
		case e := <-sink:
			if e["event"] == "health" {
				if _, ok := e["rtt_ms"]; ok {
					t.Fatal("disabled RTT leaked latency")
				}
				return
			}
		case <-deadline:
			t.Fatal("disabled RTT suppressed required health event")
		}
	}
}

func TestRuntimeExpiredMuxStartsNewLogicalSession(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, []byte(cp), 0600); err != nil {
		t.Fatal(err)
	}
	tc, err := gateway.TLS(pair, ca)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{EnableDatagrams: true})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go srv.ServeQUIC(ctx, ln)
	b := &runtimeGateway{g: NewGateway(nil), ctx: ctx}
	defer b.Close()
	budget, _ := NewTrafficBudget("expired-run", 10000)
	cfg := gatewayConfig{Transport: "quic", Endpoint: ln.Addr().String(), Hostname: "localhost", Certificate: cp, Key: kp, CA: cp}
	info := bond.PathInfo{ID: "path/0", ProfileID: "profile", Network: "wifi", Generation: 1}
	if err = b.Dial(ctx, cfg, info, budget.Bind(nil, "wifi")); err != nil {
		t.Fatal(err)
	}
	b.g.mu.Lock()
	oldToken := b.g.bondToken
	oldMux := b.g.bond
	oldMux.Close()
	b.g.mu.Unlock()
	info.Generation++
	if err = b.Dial(ctx, cfg, info, budget.Bind(nil, "wifi")); err != nil {
		t.Fatalf("expired session remained unrecoverable: %v", err)
	}
	b.g.mu.Lock()
	defer b.g.mu.Unlock()
	if b.g.bond == oldMux || b.g.bondToken == oldToken {
		t.Fatal("expired session silently reused logical identity")
	}
	if b.g.budget != budget || budget.ledger.Snapshot().Epoch != "expired-run" {
		t.Fatal("expired session reset run budget")
	}
}
func TestSingleSlotCanReturnFromLTE(t *testing.T) {
	var cfg exitRuntimeConfig
	if err := json.Unmarshal([]byte(runtimeConfig(true, "")), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Model.Profiles[0].PoolSize = 1
	cfg.InitialNetwork.Network = "cell"
	raw, _ := json.Marshal(cfg)
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("single-slot", 0)
	if err := r.SetTrafficBudget(budget); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(string(raw), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.preferred != "" })
	if err := r.UpdateNetwork(`{"network":"wifi","available":true,"allowed":true,"generation":1}`, nil); err != nil {
		t.Fatal(err)
	}
	awaitRuntime(t, func() bool { ps := b.Stats().Paths; return len(ps) == 1 && ps[0].Network == "wifi" })
}

func TestReserveProbeHonorsPhysicalNetworkPermission(t *testing.T) {
	extra := `,{"id":"reserve","exit_id":"exit","transport":"https","mode":"reserve","endpoint":"127.0.0.1:4444","priority":1,"pool_size":2,"check_reserve":true}`
	raw := strings.Replace(runtimeConfig(true, extra), `"generation":1}`, `"generation":1,"reserve_check_allowed":false}`, 1)
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("reserve-permission", 0)
	if err := r.SetTrafficBudget(budget); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(raw, nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 3 })
	time.Sleep(150 * time.Millisecond)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.checks != 0 {
		t.Fatal("optional reserve check bypassed physical network consent")
	}
}

func TestStaleWifiPreflightCannotDropLiveLTE(t *testing.T) {
	var cfg exitRuntimeConfig
	if err := json.Unmarshal([]byte(runtimeConfig(true, "")), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Model.Profiles[0].PoolSize = 1
	cfg.InitialNetwork.Network = "cell"
	raw, _ := json.Marshal(cfg)
	gate := make(chan struct{})
	entered := make(chan struct{}, 2)
	b := &runtimeTransport{checkGate: gate, checkEntered: entered}
	r := NewExitRuntime(nil)
	r.backend = b
	budget, _ := NewTrafficBudget("stale-preflight", 0)
	r.SetTrafficBudget(budget)
	if err := r.Start(string(raw), nil); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer func() { release(); r.Stop() }()
	awaitRuntime(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.preferred != "" })
	original := b.Stats().Paths[0]
	if err := r.UpdateNetwork(`{"network":"wifi","available":true,"allowed":true,"generation":1}`, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("preflight not entered")
	}
	if err := r.UpdateNetwork(`{"network":"wifi","available":true,"allowed":true,"generation":2}`, nil); err != nil {
		t.Fatal(err)
	}
	release()
	time.Sleep(150 * time.Millisecond)
	paths := b.Stats().Paths
	if len(paths) != 1 || paths[0].Network != "cell" || paths[0].Generation != original.Generation {
		t.Fatal("stale Wi-Fi success removed live LTE", paths)
	}
}
func TestStaleReserveCheckCannotPublishAvailability(t *testing.T) {
	extra := `,{"id":"reserve","exit_id":"exit","transport":"https","mode":"reserve","endpoint":"127.0.0.1:4444","priority":1,"pool_size":2,"check_reserve":true}`
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	b := &runtimeTransport{checkGate: gate, checkEntered: entered}
	sink := make(runtimeEvents, 128)
	r := NewExitRuntime(sink)
	r.backend = b
	budget, _ := NewTrafficBudget("stale-reserve", 0)
	r.SetTrafficBudget(budget)
	if err := r.Start(runtimeConfig(true, extra), nil); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer func() { release(); r.Stop() }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("reserve check not entered")
	}
	if err := r.UpdateNetwork(`{"network":"wifi","available":false,"allowed":true,"generation":2}`, nil); err != nil {
		t.Fatal(err)
	}
	release()
	time.Sleep(150 * time.Millisecond)
	for {
		select {
		case e := <-sink:
			if e["event"] == "reserve_available" || e["event"] == "reserve_unavailable" {
				t.Fatal("stale check published current availability", e)
			}
		default:
			return
		}
	}
}

type runtimeSinkFunc func(string)

func (f runtimeSinkFunc) OnEvent(raw string) { f(raw) }
func TestRuntimeCallbackCanStop(t *testing.T) {
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	done := make(chan struct{})
	var once sync.Once
	r.sink = runtimeSinkFunc(func(string) { once.Do(func() { r.Stop(); close(done) }) })
	budget, _ := NewTrafficBudget("callback-stop", 0)
	r.SetTrafficBudget(budget)
	if err := r.Start(runtimeConfig(true, ""), nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event callback deadlocked while stopping its runtime")
	}
	if len(b.Stats().Paths) != 0 {
		t.Fatal("callback stop retained live paths")
	}
}
func TestRuntimePreservesUpgradeMetadataAndStopsRetryingProfile(t *testing.T) {
	pair, cp, kp := testIdentity(t)
	var calls atomic.Int32
	caps := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		protocol.CapabilitiesHandler(protocol.Capabilities{ControlVersion: 1, DataVersion: 9, MinAndroidVersionCode: 999}).ServeHTTP(w, req)
	}))
	caps.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
	caps.StartTLS()
	defer caps.Close()
	var cfg exitRuntimeConfig
	json.Unmarshal([]byte(runtimeConfig(true, "")), &cfg)
	cfg.Gateways["auto"] = gatewayConfig{Transport: "quic", Endpoint: "127.0.0.1:4443", Hostname: "localhost", Certificate: cp, Key: kp, CA: cp, ControlURL: caps.URL + "/api/v1/capabilities", AndroidVersionCode: 45}
	raw, _ := json.Marshal(cfg)
	sink := make(runtimeEvents, 128)
	r := NewExitRuntime(sink)
	budget, _ := NewTrafficBudget("upgrade", 0)
	r.SetTrafficBudget(budget)
	if err := r.Start(string(raw), nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	deadline := time.After(2 * time.Second)
	found := false
	for !found {
		select {
		case e := <-sink:
			if e["event"] == "upgrade_required" {
				if e["data_version"] != float64(9) || e["min_android_version_code"] != float64(999) || e["exit_id"] != "exit" || e["profile_id"] != "auto" {
					t.Fatal("lost structured compatibility metadata", e)
				}
				found = true
			}
		case <-deadline:
			t.Fatal("runtime discarded upgrade-required event")
		}
	}
	time.Sleep(1300 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatal("incompatible profile was retried", calls.Load())
	}
}

func runtimeLifetimeFixture(t *testing.T, transports ...string) (gatewayConfig, string) {
	t.Helper()
	pair, cp, kp := testIdentity(t)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, []byte(cp), 0600); err != nil {
		t.Fatal(err)
	}
	tc, err := gateway.TLS(pair, ca)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := gateway.New("127.0.0.0/8", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv.BondContext = ctx
	q, err := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{EnableDatagrams: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { q.Close() })
	go srv.ServeQUIC(ctx, q)
	cfg := gatewayConfig{Transport: "quic", Endpoint: q.Addr().String(), Hostname: "localhost", Certificate: cp, Key: kp, CA: cp, BondCopyBudget: 1 << 20}
	if len(transports) > 0 && transports[0] == "https" {
		ht := tc.Clone()
		ht.NextProtos = []string{"http/1.1"}
		ln, err := tls.Listen("tcp4", "127.0.0.1:0", ht)
		if err != nil {
			t.Fatal(err)
		}
		hs := &http.Server{Handler: http.HandlerFunc(srv.ServeBondHTTPS)}
		t.Cleanup(func() { hs.Close() })
		go hs.Serve(ln)
		cfg.Transport = "https"
		cfg.Endpoint = ln.Addr().String()
	}
	model := vpnmodel.Config{Version: 1, Exits: []vpnmodel.Exit{{ID: "exit", Name: "lifetime", Kind: "demux", DemuxID: "gateway"}}, Profiles: []vpnmodel.Profile{{ID: "auto", ExitID: "exit", Transport: cfg.Transport, Mode: "auto", Endpoint: cfg.Endpoint, PoolSize: 1}}}
	raw, _ := json.Marshal(exitRuntimeConfig{Model: model, Gateways: map[string]gatewayConfig{"auto": cfg}, InitialNetwork: runtimeNetwork{Network: "wifi", Allowed: true, Available: true, Generation: 1}, Economy: true})
	return cfg, string(raw)
}
func TestExplicitRuntimeStopReleasesServerSessionCapacity(t *testing.T) {
	_, raw := runtimeLifetimeFixture(t)
	budget, _ := NewTrafficBudget("repeated-run", 1<<20)
	for i := 0; i < 7; i++ {
		r := NewExitRuntime(nil)
		r.SetTrafficBudget(budget)
		if err := r.Start(raw, nil); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(r.Stop)
		awaitRuntime(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.preferred != "" })
		started := time.Now()
		r.Stop()
		if time.Since(started) > time.Second {
			t.Fatal("explicit stop was not bounded")
		}
	}
}
func TestEphemeralChecksReleaseServerSessionCapacity(t *testing.T) {
	cfg, _ := runtimeLifetimeFixture(t)
	budget, _ := NewTrafficBudget("repeated-checks", 1<<20)
	backend := &runtimeGateway{g: NewGateway(nil), ctx: context.Background()}
	defer backend.Close()
	for i := 0; i < 7; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := backend.Check(ctx, cfg, bond.PathInfo{ID: "check", ProfileID: "auto", Network: "wifi", Generation: 1}, budget.Bind(nil, "wifi"))
		cancel()
		if err != nil {
			t.Fatalf("check %d exhausted retained server slots: %v", i+1, err)
		}
	}
}
func TestRuntimeDoesNotAccumulateBlockedDispatchers(t *testing.T) {
	b := &runtimeTransport{}
	r := NewExitRuntime(nil)
	r.backend = b
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	r.sink = runtimeSinkFunc(func(string) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	})
	budget, _ := NewTrafficBudget("blocked-callback", 0)
	r.SetTrafficBudget(budget)
	if err := r.Start(runtimeConfig(true, ""), nil); err != nil {
		t.Fatal(err)
	}
	<-entered
	r.Stop()
	defer close(release)
	err := r.Start(runtimeConfig(true, ""), nil)
	if err == nil {
		r.Stop()
		t.Fatal("restart allocated another dispatcher while previous callback remained blocked")
	}
}
func TestRuntimeForwardsGatewayDiagnosticEvents(t *testing.T) {
	_, raw := runtimeLifetimeFixture(t)
	sink := make(runtimeEvents, 128)
	r := NewExitRuntime(sink)
	budget, _ := NewTrafficBudget("diagnostic-bridge", 0)
	r.SetTrafficBudget(budget)
	if err := r.Start(raw, nil); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	awaitRuntime(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.preferred != "" })
	responder, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer responder.Close()
	go func() {
		c, e := responder.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		io.Copy(c, c)
	}()
	if err := r.Gateway().StartExitEcho(responder.Addr().String(), 60000); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(2 * time.Second)
	for {
		select {
		case e := <-sink:
			if e["event"] == "exit_echo" {
				if e["exit_id"] != "exit" || e["generation"] != float64(1) || e["connection_id"] == "" || e["error"] != nil {
					t.Fatal("diagnostic lost runtime identity or failed", e)
				}
				return
			}
		case <-timeout:
			t.Fatal("gateway diagnostic result never reached runtime event sink")
		}
	}
}
func TestRuntimeIntermittentUsefulProgressResetsBackoff(t *testing.T) {
	b := &runtimeTransport{}
	r, _ := startTestRuntime(t, b, true, "")
	awaitRuntime(t, func() bool { return len(b.Stats().Paths) == 3 })
	r.mu.Lock()
	defer r.mu.Unlock()
	var path *runtimePath
	for _, p := range r.paths {
		path = p
		break
	}
	c := r.candidate(path)
	c.failures = 6
	now := time.Now()
	for tick := 0; tick <= 310; tick++ {
		if tick%10 == 0 {
			b.mu.Lock()
			s := b.paths[path.info.ID]
			s.AckedBytes += 100
			b.paths[path.info.ID] = s
			b.mu.Unlock()
		}
		r.refresh(now.Add(time.Duration(tick) * 100 * time.Millisecond))
	}
	if c.failures != 0 {
		t.Fatal("healthy intermittent ACKs never reset runtime backoff")
	}
}
func TestRuntimeRotationPreservesDrainingFlowsAndPoolBound(t *testing.T) {
	for _, transport := range []string{"quic", "https"} {
		for _, pool := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/pool%d", transport, pool), func(t *testing.T) {
				_, raw := runtimeLifetimeFixture(t, transport)
				var cfg exitRuntimeConfig
				json.Unmarshal([]byte(raw), &cfg)
				cfg.Model.Profiles[0].PoolSize = pool
				encoded, _ := json.Marshal(cfg)
				r := NewExitRuntime(nil)
				budget, _ := NewTrafficBudget("rotate", 0)
				r.SetTrafficBudget(budget)
				if err := r.Start(string(encoded), nil); err != nil {
					t.Fatal(err)
				}
				defer r.Stop()
				awaitRuntime(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.preferred != "" && len(r.paths) == pool })
				target, err := net.Listen("tcp4", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer target.Close()
				go func() {
					for {
						c, e := target.Accept()
						if e != nil {
							return
						}
						go func() { defer c.Close(); io.Copy(c, c) }()
					}
				}()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				g := r.Gateway()
				first, err := g.dialStream(ctx, "tcp", target.Addr().String())
				if err != nil {
					t.Fatal(err)
				}
				defer first.Close()
				exchange := func(s gateway.Stream) {
					t.Helper()
					s.SetDeadline(time.Now().Add(time.Second))
					if _, e := s.Write([]byte("abc")); e != nil {
						t.Fatal(e)
					}
					buf := make([]byte, 3)
					if _, e := io.ReadFull(s, buf); e != nil || string(buf) != "abc" {
						t.Fatal("draining flow corrupted", e)
					}
				}
				exchange(first)
				g.mu.Lock()
				old := g.bond
				old.BeginDrain()
				g.mu.Unlock()
				if pool == 1 {
					if second, e := g.dialStream(ctx, "tcp", target.Addr().String()); e == nil {
						second.Close()
						t.Fatal("pool1 exceeded physical bound while oldflow active")
					}
					exchange(first)
					first.Close()
				}
				var second gateway.Stream
				for {
					second, err = g.dialStream(ctx, "tcp", target.Addr().String())
					if err == nil {
						break
					}
					if !errors.Is(err, bond.ErrDraining) || ctx.Err() != nil {
						t.Fatal("runtime rotation could not open replacement flow", err)
					}
					time.Sleep(10 * time.Millisecond)
				}
				defer second.Close()
				exchange(second)
				if pool > 1 {
					exchange(first)
				}
				g.mu.Lock()
				current := g.bond
				g.mu.Unlock()
				if old == current {
					t.Fatal("new flow reused exhausted logical mux")
				}
				physical := 0
				for _, mux := range []*bond.Mux{current, old} {
					for _, p := range mux.Session.Stats().Paths {
						if mux.Session.HasPath(p.Name) {
							physical++
						}
					}
				}
				for _, p := range current.Session.Stats().Paths {
					if current.Session.HasPath(p.Name) && old.Session.HasPath(p.Name) {
						t.Fatal("recycled slot remained physically attached to both sessions")
					}
				}
				for _, p := range r.backend.Stats().Paths {
					if p.Generation == 0 || p.ProfileID == "" {
						t.Fatal("runtime exposed detached historical path as live", p)
					}
				}
				if physical > pool {
					t.Fatalf("draining rotation exceeded globalpool: %d > %d", physical, pool)
				}
				time.Sleep(250 * time.Millisecond)
				exchange(second)
				if pool > 1 {
					exchange(first)
				}
			})
		}
	}
}

type heldRuntimePath struct {
	ctx     context.Context
	cancel  context.CancelFunc
	in, out chan []byte
}

func (p *heldRuntimePath) SendDatagram(data []byte) error {
	select {
	case p.out <- append([]byte(nil), data...):
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}
func (p *heldRuntimePath) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	select {
	case data := <-p.in:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.ctx.Done():
		return nil, p.ctx.Err()
	}
}
func (p *heldRuntimePath) Close() error { p.cancel(); return nil }
func TestRuntimeDrainRetainsAcceptedTailUntilAcknowledged(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	wire, stop := context.WithCancel(ctx)
	defer stop()
	client, server := bond.NewMux(ctx, true), bond.NewMux(ctx, false)
	defer client.Close()
	defer server.Close()
	a, b, held := make(chan []byte, 128), make(chan []byte, 128), make(chan []byte, 128)
	release := make(chan struct{})
	go func() {
		for {
			select {
			case data := <-held:
				select {
				case <-release:
				case <-wire.Done():
					return
				}
				select {
				case b <- data:
				case <-wire.Done():
					return
				}
			case <-wire.Done():
				return
			}
		}
	}()
	client.Session.AddPath("wifi", &heldRuntimePath{wire, stop, a, held})
	server.Session.AddPath("wifi", &heldRuntimePath{wire, stop, b, a})
	stream, err := client.OpenStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Write([]byte("accepted tail")); err != nil {
		t.Fatal(err)
	}
	stream.Close()
	client.BeginDrain()
	if client.Session.Stats().Pending == 0 {
		t.Fatal("fixture did not retain unacknowledged records")
	}
	g := NewGateway(nil)
	g.bondDraining = []*bond.Mux{client}
	backend := &runtimeGateway{g: g}
	backend.Stats()
	if client.Context().Err() != nil {
		t.Fatal("drain closed accepted tail while records still pending")
	}
	close(release)
	incoming, err := server.AcceptStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	incoming.SetDeadline(time.Now().Add(time.Second))
	got, err := io.ReadAll(incoming)
	if err != nil || string(got) != "accepted tail" {
		t.Fatal("accepted tail lost during drain", string(got), err)
	}
	awaitRuntime(t, func() bool { return client.Session.Stats().Pending == 0 })
	backend.Stats()
	if client.Context().Err() == nil {
		t.Fatal("fully acknowledged drain was not retired")
	}
}

func TestRuntimeRotationHonorsCallerAndStopCancellation(t *testing.T) {
	for _, stopRuntime := range []bool{false, true} {
		t.Run(fmt.Sprint(stopRuntime), func(t *testing.T) {
			_, raw := runtimeLifetimeFixture(t)
			var cfg exitRuntimeConfig
			json.Unmarshal([]byte(raw), &cfg)
			cfg.Model.Profiles[0].PoolSize = 2
			encoded, _ := json.Marshal(cfg)
			r := NewExitRuntime(nil)
			budget, _ := NewTrafficBudget("rotation-cancel", 0)
			r.SetTrafficBudget(budget)
			if err := r.Start(string(encoded), nil); err != nil {
				t.Fatal(err)
			}
			defer r.Stop()
			awaitRuntime(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.preferred != "" && len(r.paths) == 2 })
			blackhole, err := net.ListenPacket("udp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer blackhole.Close()
			entered := make(chan struct{}, 1)
			go func() {
				buf := make([]byte, 4096)
				for {
					if _, _, err := blackhole.ReadFrom(buf); err != nil {
						return
					}
					select {
					case entered <- struct{}{}:
					default:
					}
				}
			}()
			g := r.Gateway()
			g.mu.Lock()
			old := g.bond
			stream, err := old.OpenStream(context.Background())
			if err != nil {
				g.mu.Unlock()
				t.Fatal(err)
			}
			backend := r.backend.(*runtimeGateway)
			for id, path := range backend.paths {
				path.cfg.Endpoint = blackhole.LocalAddr().String()
				backend.paths[id] = path
			}
			old.BeginDrain()
			g.mu.Unlock()
			defer stream.Close()
			caller, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				s, e := g.open(caller)
				if s != nil {
					s.Close()
				}
				finished <- e
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("replacement never reached blackhole")
			}
			time.Sleep(150 * time.Millisecond) // worker can be waiting for Gateway while holding runtime mutex
			started := time.Now()
			if stopRuntime {
				done := make(chan struct{})
				go func() { r.Stop(); close(done) }()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("Stop could not cancel rotation outside runtime lock")
				}
			} else {
				cancel()
			}
			select {
			case err := <-finished:
				if err == nil {
					t.Fatal("canceled replacement succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("caller cancellation did not stop replacement dial")
			}
			if time.Since(started) > time.Second {
				t.Fatal("rotation cancellation exceeded bound")
			}
		})
	}
}
func TestRuntimeQUICJoinWelcomeHonorsCancellation(t *testing.T) {
	for _, stopRuntime := range []bool{false, true} {
		t.Run(fmt.Sprint(stopRuntime), func(t *testing.T) {
			pair, cp, kp := testIdentity(t)
			ca := filepath.Join(t.TempDir(), "ca.pem")
			os.WriteFile(ca, []byte(cp), 0600)
			tc, err := gateway.TLS(pair, ca)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := quic.ListenAddr("127.0.0.1:0", tc, &quic.Config{EnableDatagrams: true})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			serverCtx, stopServer := context.WithCancel(context.Background())
			defer stopServer()
			entered := make(chan struct{})
			go func() {
				conn, e := listener.Accept(serverCtx)
				if e != nil {
					return
				}
				defer conn.CloseWithError(0, "done")
				stream, e := conn.AcceptStream(serverCtx)
				if e != nil {
					return
				}
				var hello gateway.BondHello
				if gateway.ReadJSON(stream, &hello) != nil {
					return
				}
				close(entered)
				<-serverCtx.Done()
			}()
			cfg := gatewayConfig{Transport: "quic", Endpoint: listener.Addr().String(), Hostname: "localhost", Certificate: cp, Key: kp, CA: cp}
			done := make(chan error, 1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var runtime *ExitRuntime
			if stopRuntime {
				var model exitRuntimeConfig
				json.Unmarshal([]byte(runtimeConfig(true, "")), &model)
				model.Model.Profiles[0].Endpoint = cfg.Endpoint
				model.Gateways["auto"] = cfg
				raw, _ := json.Marshal(model)
				runtime = NewExitRuntime(nil)
				budget, _ := NewTrafficBudget("join-cancel", 0)
				runtime.SetTrafficBudget(budget)
				if err := runtime.Start(string(raw), nil); err != nil {
					t.Fatal(err)
				}
				defer runtime.Stop()
			} else {
				backend := &runtimeGateway{g: NewGateway(nil), ctx: ctx}
				defer backend.Close()
				go func() {
					done <- backend.Dial(ctx, cfg, bond.PathInfo{ID: "test", ProfileID: "auto", Network: "wifi", Generation: 1}, nil)
				}()
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("authenticated peer never received join hello")
			}
			started := time.Now()
			if stopRuntime {
				go func() { runtime.Stop(); done <- context.Canceled }()
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("withheld join welcome succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("join welcome read ignored cancellation")
			}
			if time.Since(started) > time.Second {
				t.Fatal("join cancellation exceeded bound")
			}
		})
	}
}
