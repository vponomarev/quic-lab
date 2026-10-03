package mobile

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"quiclab/internal/bond"
	"quiclab/internal/pathpolicy"
	"quiclab/internal/vpnmodel"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var runtimeConnectionSequence atomic.Uint64

type exitTransport interface {
	Dial(context.Context, gatewayConfig, bond.PathInfo, SocketBinder) error
	Remove(bond.PathInfo)
	Probe(string) error
	Stats() bond.Stats
	Close()
}
type runtimeNetwork struct {
	ReserveCheckAllowed *bool  `json:"reserve_check_allowed,omitempty"`
	Network             string `json:"network"`
	Available           bool   `json:"available"`
	Allowed             bool   `json:"allowed"`
	Generation          uint64 `json:"generation"`
	binder              SocketBinder
}
type exitRuntimeConfig struct {
	Model          vpnmodel.Config          `json:"model"`
	Gateways       map[string]gatewayConfig `json:"gateways"`
	InitialNetwork runtimeNetwork           `json:"initial_network"`
	Economy        bool                     `json:"economy"`
}
type runtimeCandidate struct {
	profile              vpnmodel.Profile
	network              string
	attempted            bool
	failures             int
	retry                time.Time
	stalled, recommended bool
	incompatible         bool
	recoveries           []time.Time
}
type runtimePath struct {
	info                                                 bond.PathInfo
	reservation                                          string
	networkGeneration                                    uint64
	health                                               pathpolicy.Health
	probes, acked                                        uint64
	successes                                            int
	healthySince, nextProbe, progressSince, lastProgress time.Time
	ready                                                bool
	draining                                             bool
}

// ExitRuntime owns one demux exit. The Android VPN run supplies its common budget.
// One worker serializes admission and dialing; Stop/UpdateNetwork cancel in-flight I/O.
type runtimeStopSignal struct{ cancel context.CancelFunc }
type ExitRuntime struct {
	stopSignal        atomic.Pointer[runtimeStopSignal]
	mu                sync.Mutex
	sink              EventSink
	backend           exitTransport
	gateway           *Gateway
	budget            *TrafficBudget
	cfg               exitRuntimeConfig
	ctx               context.Context
	cancel            context.CancelFunc
	dialCancel        context.CancelFunc
	done              chan struct{}
	dispatcherDone    chan struct{}
	wake              chan struct{}
	running           bool
	generation        uint64
	networks          map[string]runtimeNetwork
	candidates        []*runtimeCandidate
	pools             map[string]*pathpolicy.Pool
	paths             map[string]*runtimePath
	serial            map[string]uint64
	checks            map[string]time.Time
	preferred         string
	probeInterval     time.Duration
	diagnostics       bool
	connectionID      string
	sessionGeneration int64
	nextStats         time.Time
	dialNetwork       string
	events            []map[string]any
	gatewayEvents     chan map[string]any
}

func NewExitRuntime(sink EventSink) *ExitRuntime { return &ExitRuntime{sink: sink} }
func (r *ExitRuntime) SetTrafficBudget(b *TrafficBudget) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return errors.New("cannot replace budget of running exit")
	}
	if b == nil {
		return errors.New("shared run budget required")
	}
	r.budget = b
	return nil
}
func (r *ExitRuntime) Start(configJSON string, binder SocketBinder) error {
	var cfg exitRuntimeConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return err
	}
	if err := vpnmodel.Validate(cfg.Model); err != nil {
		return err
	}
	if len(cfg.Model.Exits) != 1 || cfg.Model.Exits[0].Kind != "demux" {
		return errors.New("runtime requires one demux exit; standalone uses its existing backend")
	}
	if !validRuntimeNetwork(cfg.InitialNetwork) {
		return errors.New("invalid initial network")
	}
	var identity *gatewayConfig
	for _, p := range cfg.Model.Profiles {
		if p.Mode == "disabled" {
			continue
		}
		c, ok := cfg.Gateways[p.ID]
		if !ok || c.Endpoint != p.Endpoint || c.Transport != p.Transport {
			return errors.New("missing or inconsistent profile transport")
		}
		if identity == nil {
			copy := c
			identity = &copy
		} else if c.Certificate != identity.Certificate || c.Key != identity.Key || c.CA != identity.CA || c.VerifyName != identity.VerifyName || c.Hostname != identity.Hostname {
			return errors.New("demux profiles must share gateway identity and authenticated server name")
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return errors.New("exit runtime already running")
	}
	if r.budget == nil {
		return errors.New("shared run budget required")
	}
	if r.done != nil {
		select {
		case <-r.done:
		default:
			return errors.New("previous exit generation still stopping")
		}
	}
	if r.dispatcherDone != nil {
		select {
		case <-r.dispatcherDone:
		default:
			return errors.New("previous exit callback still running")
		}
	}
	if r.probeInterval == 0 {
		r.probeInterval = time.Second
		r.diagnostics = true
	}
	r.connectionID = fmt.Sprintf("bond-%d", runtimeConnectionSequence.Add(1))
	r.nextStats = time.Time{}
	r.sessionGeneration = 0
	r.cfg = cfg
	r.generation++
	r.ctx, r.cancel = context.WithCancel(context.Background())
	r.stopSignal.Store(&runtimeStopSignal{cancel: r.cancel})
	r.done = make(chan struct{})
	r.dispatcherDone = make(chan struct{})
	r.wake = make(chan struct{}, 1)
	r.running = true
	r.networks = map[string]runtimeNetwork{}
	cfg.InitialNetwork.binder = binder
	r.networks[cfg.InitialNetwork.Network] = cfg.InitialNetwork
	r.pools = map[string]*pathpolicy.Pool{}
	r.paths = map[string]*runtimePath{}
	r.serial = map[string]uint64{}
	r.checks = map[string]time.Time{}
	r.candidates = nil
	r.preferred = ""
	r.events = nil
	sort.SliceStable(r.cfg.Model.Profiles, func(i, j int) bool { return r.cfg.Model.Profiles[i].Priority < r.cfg.Model.Profiles[j].Priority })
	for _, p := range r.cfg.Model.Profiles {
		if p.Mode == "disabled" {
			continue
		}
		r.pools[p.ID], _ = pathpolicy.NewPool(p.PoolSize)
		for _, n := range []string{"wifi", "cell"} {
			r.candidates = append(r.candidates, &runtimeCandidate{profile: p, network: n})
		}
	}
	r.gatewayEvents = make(chan map[string]any, 128)
	if _, realBackend := r.backend.(*runtimeGateway); r.backend == nil || realBackend {
		// A stopped runtime begins a new explicit lifecycle. During this
		// generation the same Gateway is retained across all transport paths.
		r.gateway = NewGateway(&runtimeGatewayEvents{ctx: r.ctx, events: r.gatewayEvents, wake: r.wake})
		r.backend = &runtimeGateway{g: r.gateway, ctx: r.ctx}
		r.gateway.runtimeBond = r.backend.(*runtimeGateway)
	}
	go r.run(r.ctx, r.done, r.dispatcherDone)
	return nil
}
func validRuntimeNetwork(n runtimeNetwork) bool {
	return (n.Network == "wifi" || n.Network == "cell") && n.Generation > 0
}
func (r *ExitRuntime) UpdateNetwork(networkJSON string, binder SocketBinder) error {
	var n runtimeNetwork
	if err := json.Unmarshal([]byte(networkJSON), &n); err != nil {
		return err
	}
	if !validRuntimeNetwork(n) {
		return errors.New("invalid network update")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return errors.New("exit runtime stopped")
	}
	old := r.networks[n.Network]
	if n.Generation < old.Generation {
		return nil
	}
	if n.Generation == old.Generation && (n.Available != old.Available || n.Allowed != old.Allowed || networkCheckPermission(n) != networkCheckPermission(old)) {
		return errors.New("changed network requires a new generation")
	}
	n.binder = binder
	r.networks[n.Network] = n
	if n.Generation != old.Generation {
		for _, c := range r.candidates {
			if c.network == n.Network {
				c.attempted = false
				c.retry = time.Time{}
			}
		}
		if r.dialCancel != nil {
			r.dialCancel()
		}
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return nil
}
func (r *ExitRuntime) Stop() {
	// A rotation may hold Gateway while the worker observes Stats under r.mu.
	// Signal cancellation before acquiring that mutex.
	if signal := r.stopSignal.Load(); signal != nil {
		signal.cancel()
	}
	r.mu.Lock()
	if !r.running {
		done := r.done
		r.mu.Unlock()
		if done != nil {
			<-done
		}
		return
	}
	r.running = false
	r.cancel()
	if r.dialCancel != nil {
		r.dialCancel()
	}
	done := r.done
	r.mu.Unlock()
	<-done
}

// Gateway stays the same across transport changes and is the router backend.
func (r *ExitRuntime) Gateway() *Gateway { r.mu.Lock(); defer r.mu.Unlock(); return r.gateway }
func (r *ExitRuntime) Snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	paths := make([]map[string]any, 0, len(r.paths))
	for _, p := range r.paths {
		paths = append(paths, map[string]any{"path_id": p.info.ID, "profile_id": p.info.ProfileID, "network": p.info.Network, "generation": p.info.Generation, "ready": p.ready})
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i]["path_id"].(string) < paths[j]["path_id"].(string) })
	id := ""
	if len(r.cfg.Model.Exits) > 0 {
		id = r.cfg.Model.Exits[0].ID
	}
	v := map[string]any{"exit_id": id, "generation": r.generation, "running": r.running, "paths": paths, "preferred": r.preferred, "connection_id": r.connectionID, "needs_cellular": r.needsCellular()}
	if r.budget != nil {
		v["budget"] = json.RawMessage(r.budget.Snapshot())
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}
func (r *ExitRuntime) event(kind string, p *runtimePath, reason string) {
	v := map[string]any{"event": kind, "exit_id": r.cfg.Model.Exits[0].ID, "generation": r.generation, "detail": reason, "connection_id": r.connectionID}
	if p != nil {
		v["profile_id"] = p.info.ProfileID
		v["path_id"] = p.info.ID
		v["path_generation"] = p.info.Generation
		v["network"] = p.info.Network
	}
	r.events = append(r.events, v)
}
func (r *ExitRuntime) allowed(network string) bool {
	n := r.networks[network]
	return n.Available && n.Allowed && (network != "cell" || r.budget.CellAllowed())
}
func (r *ExitRuntime) candidate(p *runtimePath) *runtimeCandidate {
	for _, c := range r.candidates {
		if c.profile.ID == p.info.ProfileID && c.network == p.info.Network {
			return c
		}
	}
	return nil
}
func (r *ExitRuntime) count(profile, network string) int {
	n := 0
	for _, p := range r.paths {
		if p.info.ProfileID == profile && (network == "" || p.info.Network == network) {
			n++
		}
	}
	return n
}
func (r *ExitRuntime) remove(p *runtimePath) {
	r.backend.Remove(p.info)
	r.pools[p.info.ProfileID].Release(p.reservation)
	delete(r.paths, p.info.ID)
	if r.preferred == p.info.ID {
		r.preferred = ""
	}
	r.event("path_closed", p, "")
}
func (r *ExitRuntime) fail(c *runtimeCandidate, now time.Time) {
	c.attempted = true
	c.failures++
	delay := min(30*time.Second, time.Second*time.Duration(1<<min(c.failures-1, 5)))
	c.retry = now.Add(time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4)))
}
func (r *ExitRuntime) refresh(now time.Time) {
	r.observeSession(nil)
	stats := map[string]bond.PathStats{}
	for _, s := range r.backend.Stats().Paths {
		stats[s.Name] = s
	}
	for _, p := range r.paths {
		n := r.networks[p.info.Network]
		s, exists := stats[p.info.ID]
		if exists && s.Generation > p.info.Generation && s.ProfileID == p.info.ProfileID && s.Network == p.info.Network {
			// Session rotation recycled this occupied slot without extra sockets.
			p.info.Generation = s.Generation
			r.serial[p.info.ID] = max(r.serial[p.info.ID], s.Generation)
			p.health = pathpolicy.Health{}
			p.ready = false
			p.successes = 0
			p.acked = 0
			p.probes = 0
			p.healthySince = time.Time{}
			p.progressSince = time.Time{}
			p.lastProgress = time.Time{}
			p.nextProbe = now
		}
		p.draining = s.Draining
		if !r.allowed(p.info.Network) || n.Generation != p.networkGeneration || !exists || s.Generation != p.info.Generation {
			c := r.candidate(p)
			r.remove(p)
			if r.count(c.profile.ID, c.network) == 0 {
				r.fail(c, now)
			}
			continue
		}
		progress := s.AckedBytes > p.acked || s.DataProbes > p.probes
		p.health.Observe(pathpolicy.Observation{At: now, PendingBytes: s.PendingBytes, AckedBytes: s.AckedBytes, DataProbePending: s.DataProbePending, RTT: time.Duration(s.RTTMS * float64(time.Millisecond))})
		if p.health.Stalled(now) {
			c := r.candidate(p)
			c.stalled = true
			r.remove(p)
			r.fail(c, now)
			r.event("data_stalled", p, "working-size data did not progress")
			continue
		}
		if progress {
			if r.diagnostics {
				r.event("echo", p, "")
				r.events[len(r.events)-1]["rtt_ms"] = s.RTTMS
			} else {
				r.event("health", p, "")
			}
		}
		if progress {
			c := r.candidate(p)
			if c.stalled {
				c.stalled = false
				c.recoveries = append(c.recoveries, now)
			}
			recent := c.recoveries[:0]
			for _, at := range c.recoveries {
				if now.Sub(at) <= 10*time.Minute {
					recent = append(recent, at)
				}
			}
			c.recoveries = recent
			if len(recent) >= 3 && !c.recommended && c.profile.PoolSize == 1 {
				c.recommended = true
				r.event("recommend_carousel", p, "three useful recoveries after stalled connections in ten minutes")
			}
			if p.healthySince.IsZero() {
				p.healthySince = now
			}
			p.successes++
			p.ready = true
		}
		if s.AckedBytes > p.acked {
			if p.progressSince.IsZero() || (!p.lastProgress.IsZero() && now.Sub(p.lastProgress) > 5*time.Second) {
				p.progressSince = now
			}
			p.lastProgress = now
			if now.Sub(p.progressSince) >= 30*time.Second {
				r.candidate(p).failures = 0
			}
		} else if !p.lastProgress.IsZero() && now.Sub(p.lastProgress) > 5*time.Second {
			p.progressSince = time.Time{}
		}
		p.acked, p.probes = s.AckedBytes, s.DataProbes
		if now.After(p.nextProbe) {
			if err := r.backend.Probe(p.info.ID); err != nil {
				c := r.candidate(p)
				r.remove(p)
				r.fail(c, now)
				continue
			}
			p.nextProbe = now.Add(r.probeInterval)
		}
	}
	if !now.Before(r.nextStats) {
		r.nextStats = now.Add(time.Second)
		r.event("bond_stats", nil, "")
		values := r.events[len(r.events)-1]
		v := r.backend.Stats()
		values["paths"] = v.Paths
		values["pending"] = v.Pending
		values["rescued"] = v.Rescued
		values["duplicates"] = v.Duplicates
		values["expired"] = v.Expired
		values["cell_sent"] = v.CellSent
		values["cell_blocked"] = !r.budget.CellAllowed()
		values["needs_cellular"] = r.needsCellular()
	}
	// Prefer profile priority, then Wi-Fi; returning paths must prove stability.
	var best *runtimePath
	for _, mode := range []string{"auto", "reserve"} {
		for _, c := range r.candidates {
			if c.profile.Mode != mode {
				continue
			}
			for _, p := range r.paths {
				if p.info.ProfileID == c.profile.ID && p.info.Network == c.network && p.ready && !p.draining {
					if best == nil || p.info.ID < best.info.ID {
						best = p
					}
				}
			}
			if best != nil {
				break
			}
		}
		if best != nil {
			break
		}
	}
	old := r.paths[r.preferred]
	if best != nil && (old == nil || !old.ready || old.draining || old == best || best.successes >= 3 && now.Sub(best.healthySince) >= 8*time.Second) {
		if r.preferred != best.info.ID {
			r.preferred = best.info.ID
			if b, ok := r.backend.(interface{ Prefer(string) }); ok {
				b.Prefer(best.info.ID)
			}
			r.event("connected", best, "usable demux data path")
		}
	}
	if active := r.paths[r.preferred]; active != nil && r.candidate(active).profile.Mode == "auto" {
		for _, p := range r.paths {
			if r.candidate(p).profile.Mode == "reserve" {
				r.remove(p)
			}
		}
	}
}

// Economy permits cold LTE only after every eligible automatic Wi-Fi candidate
// has been tried. A returned Wi-Fi path is verified before dropping live LTE.
func (r *ExitRuntime) cellEligible() bool {
	if !r.allowed("cell") {
		return false
	}
	return r.needsCellular()
}
func (r *ExitRuntime) needsCellular() bool {
	if r.budget == nil || !r.budget.CellAllowed() {
		return false
	}
	if !r.cfg.Economy {
		return true
	}
	if p := r.paths[r.preferred]; p != nil && p.info.Network == "cell" {
		return true
	}
	if !r.allowed("wifi") {
		return true
	}
	if r.dialNetwork == "wifi" {
		return false
	}
	for _, p := range r.paths {
		if p.info.Network == "wifi" {
			return false
		}
	}
	for _, c := range r.candidates {
		if c.network == "wifi" && c.profile.Mode == "auto" && (!c.attempted || r.count(c.profile.ID, "wifi") > 0) {
			return false
		}
	}
	return true
}
func (r *ExitRuntime) eligible(c *runtimeCandidate) bool {
	return !c.incompatible && r.allowed(c.network) && (c.network != "cell" || r.cellEligible())
}
func (r *ExitRuntime) selectDial(now time.Time) *runtimeCandidate {
	// All automatic profile/network attempts precede reserve admission.
	autoLive := false
	for _, c := range r.candidates {
		if c.profile.Mode == "auto" && r.eligible(c) && (!c.attempted || r.count(c.profile.ID, c.network) > 0) {
			autoLive = true
		}
	}
	for _, mode := range []string{"auto", "reserve"} {
		if mode == "reserve" && autoLive {
			break
		}
		for _, c := range r.candidates {
			if c.profile.Mode != mode || !r.eligible(c) || now.Before(c.retry) {
				continue
			}
			total := r.count(c.profile.ID, "")
			same := r.count(c.profile.ID, c.network)
			desired := c.profile.PoolSize
			other := "cell"
			if c.network == "cell" {
				other = "wifi"
			}
			if r.allowed("wifi") && r.cellEligible() && c.profile.PoolSize >= 2 {
				if c.network == "wifi" {
					desired = c.profile.PoolSize - 1
				} else {
					desired = 1
				}
			}
			if same >= desired {
				continue
			}
			// A lower priority profile is admitted only when no better live candidate exists.
			betterLive := false
			for _, x := range r.candidates {
				if x == c {
					break
				}
				if x.profile.Mode == mode && x.profile.ID != c.profile.ID && r.count(x.profile.ID, "") > 0 {
					betterLive = true
					break
				}
			}
			if betterLive {
				continue
			}
			if total >= c.profile.PoolSize {
				// Move one spare slot, leaving the current useful path until replacement works.
				var spare *runtimePath
				for _, p := range r.paths {
					if p.info.ProfileID == c.profile.ID && p.info.Network == other && p.info.ID != r.preferred {
						if spare == nil || p.info.ID > spare.info.ID {
							spare = p
						}
					}
				}
				if spare == nil {
					continue
				}
				r.remove(spare)
			}
			return c
		}
	}
	return nil
}
func (r *ExitRuntime) run(ctx context.Context, done, dispatcherDone chan struct{}) {
	// A bounded dispatcher isolates application callbacks from the worker. Stop
	// waits only for the worker, so a callback may safely call Stop itself.
	queue := make(chan string, 128)
	sink := r.sink
	go func() {
		defer close(dispatcherDone)
		for {
			select {
			case <-ctx.Done():
				return
			case raw := <-queue:
				if ctx.Err() != nil {
					return
				}
				if sink != nil {
					sink.OnEvent(raw)
				}
			}
		}
	}()
	defer close(done)
	defer func() {
		// Notify the server while authenticated transports are still usable.
		r.backend.Close()
		r.mu.Lock()
		for _, p := range r.paths {
			r.remove(p)
		}
		r.mu.Unlock()
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		r.mu.Lock()
		now := time.Now()
		r.collectGatewayEvents()
		r.refresh(now)
		// Once stable Wi-Fi has won in economy, stop every cellular warm socket.
		if p := r.paths[r.preferred]; r.cfg.Economy && p != nil && p.info.Network == "wifi" {
			for _, path := range r.paths {
				if path.info.Network == "cell" {
					r.remove(path)
				}
			}
		}
		c := r.selectDial(now)
		var path *runtimePath
		var cfg gatewayConfig
		var binder SocketBinder
		var dialctx context.Context
		var cancel context.CancelFunc
		if c != nil {
			reservation, err := r.pools[c.profile.ID].Reserve(c.profile.ID, c.network)
			if err == nil {
				// Fixed slot IDs keep bond's lifetime identity table bounded under reconnects.
				index := 0
				for ; index < c.profile.PoolSize; index++ {
					if r.paths[runtimeWirePathID(c.profile.ID, index)] == nil {
						break
					}
				}
				id := runtimeWirePathID(c.profile.ID, index)
				r.serial[id]++
				path = &runtimePath{info: bond.PathInfo{ID: id, ProfileID: c.profile.ID, Network: c.network, Generation: r.serial[id]}, reservation: reservation, networkGeneration: r.networks[c.network].Generation}
				cfg = r.cfg.Gateways[c.profile.ID]
				binder = r.budget.Bind(r.networks[c.network].binder, c.network)
				dialctx, cancel = context.WithTimeout(ctx, 3*time.Second)
				r.dialCancel = cancel
				r.dialNetwork = c.network
				c.attempted = true
				r.event("connecting", path, "")
			}
		}
		var check *runtimeCandidate
		returnCheck := false
		checkGeneration := uint64(0)
		if path == nil {
			if _, ok := r.backend.(interface {
				Check(context.Context, gatewayConfig, bond.PathInfo, SocketBinder) error
			}); ok {
				// A single-slot profile cannot hold a prepared backup. Verify returning Wi-Fi
				// separately, then replace its LTE slot while retaining the logical mux.
				for _, candidate := range r.candidates {
					if !candidate.incompatible && candidate.network == "wifi" && candidate.profile.PoolSize == 1 && r.allowed("wifi") && r.count(candidate.profile.ID, "cell") == 1 && !now.Before(candidate.retry) && !now.Before(r.checks["return/"+candidate.profile.ID]) {
						check = candidate
						returnCheck = true
						r.checks["return/"+candidate.profile.ID] = now.Add(time.Second)
						break
					}
				}
				if check != nil {
					cfg = r.cfg.Gateways[check.profile.ID]
					binder = r.budget.Bind(r.networks[check.network].binder, check.network)
					dialctx, cancel = context.WithTimeout(ctx, 3*time.Second)
					r.dialCancel = cancel
				}
				for _, candidate := range r.candidates {
					if check != nil {
						break
					}
					if candidate.profile.Mode == "reserve" && candidate.profile.CheckReserve && networkCheckPermission(r.networks[candidate.network]) && r.eligible(candidate) && r.count(candidate.profile.ID, "") == 0 && !now.Before(r.checks[candidate.profile.ID]) {
						check = candidate
						r.checks[candidate.profile.ID] = now.Add(time.Duration(float64(time.Minute) * (0.8 + rand.Float64()*0.4)))
						cfg = r.cfg.Gateways[candidate.profile.ID]
						binder = r.budget.Bind(r.networks[candidate.network].binder, candidate.network)
						dialctx, cancel = context.WithTimeout(ctx, 3*time.Second)
						r.dialCancel = cancel
						break
					}
				}
			}
		}
		if check != nil {
			checkGeneration = r.networks[check.network].Generation
		}
		events := r.events
		r.events = nil
		r.mu.Unlock()
		for _, v := range events {
			if r.sink != nil && ctx.Err() == nil {
				raw, _ := json.Marshal(v)
				select {
				case queue <- string(raw):
				default:
					// Bound memory even if a consumer stalls. Keep the most recent state.
					select {
					case <-queue:
					default:
					}
					select {
					case queue <- string(raw):
					default:
					}
				}
			}
		}
		if path != nil {
			err := r.backend.Dial(dialctx, cfg, path.info, binder)
			cancel()
			r.mu.Lock()
			r.dialCancel = nil
			r.dialNetwork = ""
			stale := ctx.Err() != nil || !r.running || r.networks[c.network].Generation != path.networkGeneration || !r.allowed(c.network)
			if err != nil || stale {
				r.backend.Remove(path.info)
				r.pools[c.profile.ID].Release(path.reservation)
				if !stale && !r.rejectUpgrade(c, path, err) {
					r.fail(c, time.Now())
					r.event("operation_failed", path, "transport connection failed")
				}
			} else {
				r.observeSession(path)
				r.paths[path.info.ID] = path
				_ = r.backend.Probe(path.info.ID)
				path.nextProbe = time.Now().Add(r.probeInterval)
			}
			r.mu.Unlock()
			continue
		}
		if check != nil {
			info := bond.PathInfo{ID: "reserve-check", ProfileID: check.profile.ID, Network: check.network, Generation: 1}
			err := r.backend.(interface {
				Check(context.Context, gatewayConfig, bond.PathInfo, SocketBinder) error
			}).Check(dialctx, cfg, info, binder)
			cancel()
			r.mu.Lock()
			r.dialCancel = nil
			r.dialNetwork = ""
			if ctx.Err() == nil && r.networks[check.network].Generation == checkGeneration && !r.rejectUpgrade(check, &runtimePath{info: info}, err) {
				if returnCheck {
					if err == nil && r.allowed("wifi") {
						for _, p := range r.paths {
							if p.info.ProfileID == check.profile.ID && p.info.Network == "cell" {
								r.remove(p)
							}
						}
						check.attempted = false
						check.retry = time.Time{}
					} else {
						r.fail(check, time.Now())
					}
				} else {
					kind := "reserve_available"
					if err != nil {
						kind = "reserve_unavailable"
					}
					r.event(kind, &runtimePath{info: info}, "ephemeral availability check")
				}
			}
			r.mu.Unlock()
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		case <-ticker.C:
		}
	}
}

// SetRTT configures diagnostic cadence without disabling liveness detection.
func (r *ExitRuntime) SetRTT(enabled bool, intervalMS int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.diagnostics = enabled
	r.probeInterval = time.Duration(max(1000, min(intervalMS, 5000))) * time.Millisecond
	if !enabled {
		r.probeInterval = 5 * time.Second
	}
	for _, p := range r.paths {
		p.nextProbe = time.Now().Add(r.probeInterval)
	}
}

func runtimeWirePathID(profileID string, slot int) string {
	digest := sha256.Sum256([]byte(profileID))
	return fmt.Sprintf("%x/%d", digest[:16], slot)
}

func networkCheckPermission(n runtimeNetwork) bool {
	return n.ReserveCheckAllowed == nil || *n.ReserveCheckAllowed
}

// rejectUpgrade disables only the incompatible profile; other profiles and exits
// continue independently. Metadata is retained for the Android update action.
func (r *ExitRuntime) rejectUpgrade(c *runtimeCandidate, p *runtimePath, err error) bool {
	var upgrade *runtimeUpgradeError
	if !errors.As(err, &upgrade) {
		return false
	}
	for _, candidate := range r.candidates {
		if candidate.profile.ID == c.profile.ID {
			candidate.incompatible = true
		}
	}
	r.event("upgrade_required", p, "client/server data protocol incompatible")
	event := r.events[len(r.events)-1]
	event["min_android_version_code"] = upgrade.minAndroid
	event["data_version"] = upgrade.dataVersion
	return true
}

// Diagnostic producers may emit while holding Gateway locks. Queue without
// taking runtime locks; the worker scopes events and the dispatcher calls out.
type runtimeGatewayEvents struct {
	ctx    context.Context
	events chan map[string]any
	wake   chan struct{}
}

func (s *runtimeGatewayEvents) OnEvent(raw string) {
	if s.ctx.Err() != nil {
		return
	}
	var event map[string]any
	if json.Unmarshal([]byte(raw), &event) != nil {
		return
	}
	select {
	case s.events <- event:
	default:
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (r *ExitRuntime) collectGatewayEvents() {
	for {
		select {
		case event := <-r.gatewayEvents:
			event["exit_id"] = r.cfg.Model.Exits[0].ID
			event["generation"] = r.generation
			event["connection_id"] = r.connectionID
			r.events = append(r.events, event)
		default:
			return
		}
	}
}
func (r *ExitRuntime) observeSession(path *runtimePath) {
	backend, ok := r.backend.(interface{ SessionGeneration() int64 })
	if !ok {
		return
	}
	session := backend.SessionGeneration()
	if session == 0 {
		return
	}
	if r.sessionGeneration > 0 && session != r.sessionGeneration {
		previous := r.connectionID
		r.connectionID = fmt.Sprintf("bond-%d", runtimeConnectionSequence.Add(1))
		kind, reason := "session_lost", "previous demux session expired; previous application flows are closed"
		if backend, ok := r.backend.(interface{ SessionRotated() bool }); ok && backend.SessionRotated() {
			kind = "session_rotated"
			reason = "flow IDs exhausted; old flows drain within the existing connection pool"
		}
		r.event(kind, path, reason)
		r.events[len(r.events)-1]["previous_connection_id"] = previous
	}
	r.sessionGeneration = session
}
