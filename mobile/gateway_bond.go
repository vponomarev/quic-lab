package mobile

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/quic-go/quic-go"
	"net"
	"quiclab/internal/bond"
	"quiclab/internal/bondquic"
	"quiclab/internal/gateway"
	"quiclab/internal/pathpolicy"
	"quiclab/internal/servertls"
	"sort"
	"time"
)

func (g *Gateway) startBond(binder SocketBinder) error {
	if err := g.checkCapabilities(binder); err != nil {
		return err
	}
	if g.budget != nil && !g.budget.CellAllowed() {
		g.bondCellBlocked = true
	}
	newContext := g.ctx == nil || g.ctx.Err() != nil
	if newContext {
		g.ctx, g.cancel = context.WithCancel(context.Background())
	}
	g.bond = bond.NewMux(g.ctx, true)
	remaining := g.cfg.BondCellBudget
	if remaining > 0 {
		if g.bondCellUsed >= remaining {
			g.bondCellBlocked = true
			remaining = 0
		} else {
			remaining -= g.bondCellUsed
		}
	}
	g.bond.Session.Configure(g.cfg.BondCopyBudget, remaining)
	if g.bondCellBlocked {
		g.bond.Session.BlockCell()
	}
	var secret [32]byte
	if _, e := rand.Read(secret[:]); e != nil {
		g.bond.Close()
		g.bond = nil
		if newContext {
			g.cancel()
			g.cancel = nil
		}
		return e
	}
	g.bondToken = hex.EncodeToString(secret[:])
	name := g.cfg.InitialPath
	if name == "" {
		name = "wifi"
	}
	if e := g.addBondPath(name, binder, true); e != nil {
		g.bond.Close()
		g.bond = nil
		if newContext {
			g.cancel()
			g.cancel = nil
		}
		return e
	}
	g.datagrams = gateway.NewPacketMux(g.bond)
	g.session++
	g.emit("connected", map[string]any{"session": g.session, "transport": g.cfg.Transport, "detail": "Independent paths sharing one bond session"})
	go g.bondStats(g.ctx, g.bond, g.bondCellUsed, g.session)
	if newContext && g.cfg.TransitEndpoint != "" {
		go g.transitHeartbeat(g.ctx, nil, g.cfg.TransitEndpoint)
	}
	return nil
}
func (g *Gateway) addBondPath(name string, binder SocketBinder, create bool) error {
	g.bondPathGeneration++
	return g.addBondNamedPath(g.ctx, bond.PathInfo{ID: name, ProfileID: "legacy", Network: name, Generation: g.bondPathGeneration}, binder, create)
}
func (g *Gateway) addBondNamedPath(ctx context.Context, info bond.PathInfo, binder SocketBinder, create bool) error {
	name := info.Network
	if g.budget != nil {
		b, ok := binder.(*budgetBinder)
		if !ok || b.budget != g.budget || b.network != name {
			return errors.New("bond path must use the run budget and physical network")
		}
	}
	if g.cfg.Transport == "https" {
		return g.addBondHTTPS(ctx, g.cfg.Endpoint, info, binder, create)
	}
	host, _, _ := net.SplitHostPort(g.cfg.Endpoint)
	tr, e := openTransport(net.ParseIP(host), binder, nil)
	if e != nil {
		return e
	}
	cleanup := func() { tr.close() }
	tc := g.tls.Clone()
	tc.NextProtos = []string{bondquic.ALPN}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	remote, e := net.ResolveUDPAddr("udp4", g.cfg.Endpoint)
	if e != nil {
		cleanup()
		return e
	}
	c, e := tr.q.Dial(ctx, remote, tc, &quic.Config{EnableDatagrams: true, MaxIdleTimeout: 10 * time.Second, MaxIncomingStreams: 0, MaxIncomingUniStreams: -1})
	if e != nil {
		cleanup()
		return e
	}
	fail := func(err error) error { c.CloseWithError(1, "join failed"); cleanup(); return err }
	st, e := c.OpenStreamSync(ctx)
	if e != nil {
		return fail(e)
	}
	defer st.Close()
	deadline := time.Now().Add(3 * time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	st.SetDeadline(deadline)
	interrupted := make(chan struct{})
	stopInterrupt := context.AfterFunc(ctx, func() {
		st.CancelRead(0)
		st.CancelWrite(0)
		close(interrupted)
	})
	defer func() {
		if !stopInterrupt() {
			<-interrupted
		}
	}()
	if e = gateway.WriteJSON(st, gateway.BondHello{PathID: info.ID, ProfileID: info.ProfileID, Network: info.Network, Generation: info.Generation, Token: g.bondToken, Path: name, Create: create, CopyBudget: g.cfg.BondCopyBudget, CellBudget: g.remainingBondBudget(), CellDisabled: g.bondCellBlocked}); e != nil {
		return fail(e)
	}
	var reply gateway.BondWelcome
	if e = gateway.ReadJSON(st, &reply); e != nil {
		return fail(e)
	}
	if reply.Error != "" {
		if reply.Error == "LTE session budget exhausted" {
			g.bondCellBlocked = true
			g.bond.Session.BlockCell()
		}
		return fail(errors.New(reply.Error))
	}
	if create {
		secret, err := hex.DecodeString(reply.Token)
		if err != nil || len(secret) != 32 {
			return fail(errors.New("invalid session token"))
		}
		g.bondToken = reply.Token
	}
	if !c.ConnectionState().SupportsDatagrams.Remote {
		return fail(errors.New("bond requires QUIC DATAGRAM"))
	}
	path := bondquic.NewQUICPath(c, cleanup)
	if e = g.bond.Session.AddNamedPath(info, path); e != nil {
		path.Close()
		return e
	}
	if g.bondBinders == nil {
		g.bondBinders = map[string]SocketBinder{}
	}
	g.bondBinders[info.ID] = binder
	return nil
}

// EnsureBondPath reconnects a missing path without replacing the shared session.
func (g *Gateway) EnsureBondPath(name string, binder SocketBinder) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.bond == nil {
		return errors.New("maximum availability is not running")
	}
	if name == "cell" && ((g.budget != nil && !g.budget.CellAllowed()) || !g.bond.Session.CellAllowed()) {
		return errors.New("LTE session budget exhausted")
	}
	if g.bond.Session.HasPath(name) {
		return nil
	}
	return g.addBondPath(name, binder, false)
}
func (g *Gateway) DropBondPath(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if name == "cell" && g.budget != nil && !g.budget.CellAllowed() {
		g.bondCellBlocked = true
		if g.bond != nil {
			g.bond.Session.BlockCellAndNotify()
		}
		for _, m := range g.bondDraining {
			m.Session.BlockCellAndNotify()
			m.Session.RemovePath("cell")
		}
	}
	if g.bond != nil {
		g.bond.Session.RemovePath(name)
	}
}
func (g *Gateway) bondStats(ctx context.Context, m *bond.Mux, offset uint64, session int64) {
	health := make(map[string]*pathpolicy.Health)
	generations := make(map[string]uint64)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.Context().Done():
			g.mu.Lock()
			current := g.bond == m
			g.mu.Unlock()
			if !current {
				return
			}
			g.emit("disconnected", map[string]any{"detail": "Both paths failed; availability session expired"})
			return
		case <-t.C:
			g.mu.Lock()
			current := g.bond == m
			g.mu.Unlock()
			if !current {
				return
			}
			if ctx.Err() != nil {
				return
			}
			v := m.Session.Stats()
			stalled := make([]string, 0)
			for _, p := range v.Paths {
				if p.Generation == 0 {
					delete(health, p.Name)
					delete(generations, p.Name)
					continue
				}
				h := health[p.Name]
				if h == nil || generations[p.Name] != p.Generation {
					h = &pathpolicy.Health{}
					health[p.Name] = h
					generations[p.Name] = p.Generation
				}
				now := time.Now()
				h.Observe(pathpolicy.Observation{PathID: p.Name, Generation: p.Generation, At: now, PendingBytes: p.PendingBytes, AckedBytes: p.AckedBytes, DataProbePending: p.DataProbePending, RTT: time.Duration(p.RTTMS * float64(time.Millisecond))})
				if h.Stalled(now) {
					stalled = append(stalled, p.Name)
				}
			}
			g.emit("bond_stats", map[string]any{"data_stalled_paths": stalled, "cell_sent": v.CellSent + offset, "cell_blocked": v.CellBlocked, "paths": v.Paths, "pending": v.Pending, "oldest_ms": v.OldestMS, "rescued": v.Rescued, "duplicates": v.Duplicates, "expired": v.Expired})
			for _, p := range v.Paths {
				if p.Ready {
					g.emit("echo", map[string]any{"rtt_ms": p.RTTMS, "connection_id": fmt.Sprintf("bond-%d", session), "detail": "availability path health"})
					break
				}
			}
		}
	}
}

type bondUDP struct{ g *Gateway }

func (d bondUDP) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "udp4" {
		return nil, errors.New("UDP only")
	}
	d.g.mu.Lock()
	m, packets := d.g.bond, d.g.datagrams
	d.g.mu.Unlock()
	if m == nil || packets == nil {
		return nil, net.ErrClosed
	}
	st, e := m.OpenStream(ctx)
	if e == bond.ErrDraining {
		d.g.mu.Lock()
		if d.g.bond == m {
			e = d.g.rotateBondLocked(ctx, m)
		} else {
			e = nil
		}
		m, packets = d.g.bond, d.g.datagrams
		d.g.mu.Unlock()
		if e != nil {
			return nil, e
		}
		if m == nil || packets == nil {
			return nil, net.ErrClosed
		}
		st, e = m.OpenStream(ctx)
	}
	if e != nil {
		return nil, e
	}
	return packets.DialPacketUDP(ctx, st, address)
}

func (g *Gateway) BondCellAllowed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return (g.budget == nil || g.budget.CellAllowed()) && !g.bondCellBlocked && (g.bond == nil || g.bond.Session.CellAllowed())
}

func (g *Gateway) RestartBond(name string, binder SocketBinder) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.cfg.MaxAvailability {
		return errors.New("not a bonded session")
	}
	if name != "wifi" && name != "cell" {
		return errors.New("invalid path")
	}
	if g.bond != nil {
		snapshot := g.bond.Session.Stats()
		g.bondCellUsed += snapshot.CellSent
		g.bondCellBlocked = g.bondCellBlocked || snapshot.CellBlocked
		g.bond.Close()
	}
	if g.cancel != nil {
		g.cancel()
	}
	g.cfg.InitialPath = name
	g.datagrams = nil
	return g.startBond(binder)
}

func (g *Gateway) remainingBondBudget() uint64 {
	if g.cfg.BondCellBudget > g.bondCellUsed {
		return g.cfg.BondCellBudget - g.bondCellUsed
	}
	return 0
}

// Called under g.mu. The root context/TUN survives; retired sessions keep their
// existing sockets until their bounded drain timeout or whole-Gateway Stop.
func (g *Gateway) rotateBondLocked(ctx context.Context, old *bond.Mux) error {
	if g.runtimeBond != nil {
		return g.runtimeBond.rotateLocked(ctx, old)
	}
	// Legacy callers without a shared socket budget must not reset their finite limit.
	if g.cfg.BondCellBudget > 0 {
		return errors.New("bond rotation with finite LTE budget requires shared ledger")
	}
	live := g.bondDraining[:0]
	for _, m := range g.bondDraining {
		if m.Context().Err() == nil {
			live = append(live, m)
		}
	}
	g.bondDraining = live
	if len(live) >= 3 {
		return errors.New("bond draining session limit")
	}
	name := "wifi"
	if !old.Session.HasPath(name) {
		name = "cell"
	}
	binder, ok := g.bondBinders[name]
	if !ok || !old.Session.HasPath(name) {
		return errors.New("no path for replacement session")
	}
	token, packets := g.bondToken, g.datagrams
	previousPath := g.cfg.InitialPath
	g.cfg.InitialPath = name
	if err := g.startBond(binder); err != nil {
		g.bond = old
		g.bondToken = token
		g.datagrams = packets
		g.cfg.InitialPath = previousPath
		return err
	}
	g.bondDraining = append(g.bondDraining, old)
	return nil
}

// runtimeGateway keeps the logical mux and token while individual QUIC/HTTPS
// transports change. Temporary per-dial settings never replace the exit backend.
type runtimeGateway struct {
	paths              map[string]runtimeBoundPath
	preferred          string
	rotationGeneration int64
	g                  *Gateway
	ctx                context.Context
}

func (b *runtimeGateway) Dial(ctx context.Context, cfg gatewayConfig, info bond.PathInfo, binder SocketBinder) error {
	g := b.g
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(cfg.Endpoint)
	if err != nil || net.ParseIP(host).To4() == nil {
		return errors.New("numeric IPv4 endpoint required")
	}
	pair, err := tls.X509KeyPair([]byte(cfg.Certificate), []byte(cfg.Key))
	if err != nil {
		return fmt.Errorf("client identity: %w", err)
	}
	sni, verify, err := cfg.tlsNames()
	if err != nil {
		return err
	}
	var roots *x509.CertPool
	if cfg.CA != "" {
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(cfg.CA)) {
			return errors.New("invalid server CA")
		}
	}
	tc, err := servertls.ClientConfig(servertls.ClientOptions{ServerName: sni, VerifyName: verify, Roots: roots, Certificate: &pair})
	if err != nil {
		return err
	}
	if g.bond != nil && g.bond.Context().Err() != nil {
		g.bond.Close()
		g.bond = nil
		g.datagrams = nil
		g.bondBinders = nil

	}
	create := g.bond == nil
	if create {
		// Logical state outlives worker cancellation so explicit Stop can notify
		// the server before closing authenticated paths. Dials still use ctx.
		if g.ctx == nil || g.ctx.Err() != nil {
			g.ctx, g.cancel = context.WithCancel(context.Background())
		}
		g.cfg = cfg
		g.cfg.MaxAvailability = true
		g.cfg.BondCellBudget = 0
		g.tls = tc
		if bb, ok := binder.(*budgetBinder); ok {
			g.budget = bb.budget
		}
		g.bond = bond.NewMux(g.ctx, true)
		g.bond.Session.Configure(cfg.BondCopyBudget, 0)
		var secret [32]byte
		if _, err = rand.Read(secret[:]); err != nil {
			g.bond.Close()
			g.bond = nil

			return err
		}
		g.bondToken = hex.EncodeToString(secret[:])
	}
	dial := &Gateway{ctx: ctx, cfg: cfg, tls: tc, bond: g.bond, bondToken: g.bondToken, budget: g.budget}
	dial.cfg.BondCellBudget = 0
	if err = dial.checkCapabilities(binder); err == nil {
		err = dial.addBondNamedPath(ctx, info, binder, create)
	}
	if err != nil {
		if create || err.Error() == "session expired" {
			g.bond.Close()
			g.bond = nil
			g.datagrams = nil
		}
		return err
	}
	if err = ctx.Err(); err != nil {
		if create {
			g.terminateBondsLocked()
		}
		g.bond.Session.RemoveNamedPath(info)
		if create {
			g.bond.Close()
			g.bond = nil
		}
		return err
	}
	g.bondToken = dial.bondToken
	if b.paths == nil {
		b.paths = map[string]runtimeBoundPath{}
	}
	b.paths[info.ID] = runtimeBoundPath{mux: g.bond, info: info, cfg: cfg, tls: tc, binder: binder}
	if g.bondBinders == nil {
		g.bondBinders = map[string]SocketBinder{}
	}
	g.bondBinders[info.ID] = binder
	if create {
		g.datagrams = gateway.NewPacketMux(g.bond)
		g.session++
	}
	return nil
}
func (b *runtimeGateway) Remove(info bond.PathInfo) {
	b.g.mu.Lock()
	defer b.g.mu.Unlock()
	if path, ok := b.paths[info.ID]; ok && path.info.Generation == info.Generation {
		path.mux.Session.RemoveNamedPath(info)
		delete(b.paths, info.ID)
		delete(b.g.bondBinders, info.ID)
	}
}
func (b *runtimeGateway) Probe(id string) error {
	b.g.mu.Lock()
	defer b.g.mu.Unlock()
	if path, ok := b.paths[id]; ok {
		return path.mux.Session.ProbeData(id)
	}
	return net.ErrClosed
}
func (b *runtimeGateway) Stats() bond.Stats {
	b.g.mu.Lock()
	defer b.g.mu.Unlock()
	var result bond.Stats
	sessions := append([]*bond.Mux{}, b.g.bondDraining...)
	if b.g.bond != nil {
		sessions = append(sessions, b.g.bond)
	}
	for _, m := range sessions {
		if m != b.g.bond && m.Context().Err() == nil && m.ActiveFlows() == 0 && m.Session.Stats().Pending == 0 {
			m.CloseGracefully()
		}
		snapshot := m.Session.Stats()
		for _, path := range snapshot.Paths {
			if m.Session.HasPath(path.Name) {
				path.Draining = m != b.g.bond
				result.Paths = append(result.Paths, path)
			}
		}
		result.Pending += snapshot.Pending
		result.Duplicates += snapshot.Duplicates
		result.Rescued += snapshot.Rescued
		result.Expired += snapshot.Expired
		result.CellSent += snapshot.CellSent
		result.CellBlocked = result.CellBlocked || snapshot.CellBlocked
		result.OldestMS = max(result.OldestMS, snapshot.OldestMS)
	}
	for id, path := range b.paths {
		if !path.mux.Session.HasPath(id) {
			delete(b.paths, id)
		}
	}
	return result
}
func (b *runtimeGateway) Close() { b.g.Stop() }
func (b *runtimeGateway) Prefer(id string) {
	b.g.mu.Lock()
	defer b.g.mu.Unlock()
	b.preferred = id
	if path, ok := b.paths[id]; ok {
		path.mux.Session.PreferPath(id)
	}
}
func (b *runtimeGateway) Check(ctx context.Context, cfg gatewayConfig, info bond.PathInfo, binder SocketBinder) error {
	probe := &runtimeGateway{g: NewGateway(nil), ctx: ctx}
	defer probe.Close()
	if err := probe.Dial(ctx, cfg, info, binder); err != nil {
		return err
	}
	if err := probe.Probe(info.ID); err != nil {
		return err
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		for _, p := range probe.Stats().Paths {
			if p.Name == info.ID && p.DataProbes > 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (b *runtimeGateway) SessionGeneration() int64 {
	b.g.mu.Lock()
	defer b.g.mu.Unlock()
	return b.g.session
}

// terminateBondsLocked bounds the complete explicit-stop notification, including
// draining sessions, under one deadline. Transport loss does not call this.
func (g *Gateway) terminateBondsLocked() {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if g.bond != nil {
		g.bond.Session.Terminate(ctx)
	}
	for _, old := range g.bondDraining {
		old.Session.Terminate(ctx)
	}
}

type runtimeBoundPath struct {
	mux    *bond.Mux
	info   bond.PathInfo
	cfg    gatewayConfig
	tls    *tls.Config
	binder SocketBinder
}

// rotateLocked reuses an already occupied slot. Draining paths remain counted;
// a one-slot profile cannot admit new flows until its old flows finish or expire.
func (b *runtimeGateway) rotateLocked(caller context.Context, old *bond.Mux) error {
	ctx, cancel := context.WithTimeout(caller, 3*time.Second)
	defer cancel()
	if b.ctx != nil {
		stop := context.AfterFunc(b.ctx, cancel)
		defer stop()
		if b.ctx.Err() != nil {
			return b.ctx.Err()
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	g := b.g
	live := g.bondDraining[:0]
	for _, m := range g.bondDraining {
		if m.Context().Err() == nil {
			live = append(live, m)
		}
	}
	g.bondDraining = live
	if len(live) >= 3 {
		return errors.New("bond draining session limit")
	}
	var candidates []runtimeBoundPath
	for _, path := range b.paths {
		if path.mux == old && old.Session.HasPath(path.info.ID) {
			candidates = append(candidates, path)
		}
	}
	if len(candidates) == 0 {
		return errors.New("no path for replacement session")
	}
	// Deterministic spare selection retains the active path on the old mux.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].info.ID < candidates[j].info.ID })
	chosen := candidates[0]
	active := old.ActiveFlows() > 0 || old.Session.Stats().Pending > 0
	if active {
		if len(candidates) < 2 {
			return bond.ErrDraining
		}
		found := false
		preferredProfile := chosen.info.ProfileID
		if preferred, ok := b.paths[b.preferred]; ok && preferred.mux == old {
			preferredProfile = preferred.info.ProfileID
		}
		for _, path := range candidates {
			if path.info.ID != b.preferred && path.info.ProfileID == preferredProfile {
				chosen = path
				found = true
				break
			}
		}
		if !found {
			return bond.ErrDraining
		}
		old.Session.RemoveNamedPath(chosen.info)
		delete(b.paths, chosen.info.ID)
	} else {
		old.CloseGracefully()
		for _, path := range candidates {
			delete(b.paths, path.info.ID)
		}
	}
	oldToken, oldPackets, oldCfg, oldTLS := g.bondToken, g.datagrams, g.cfg, g.tls
	replacement := bond.NewMux(g.ctx, true)
	replacement.Session.Configure(chosen.cfg.BondCopyBudget, 0)
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		replacement.Close()
		return err
	}
	g.bond = replacement
	g.bondToken = hex.EncodeToString(secret[:])
	g.cfg = chosen.cfg
	g.cfg.MaxAvailability = true
	g.cfg.BondCellBudget = 0
	g.tls = chosen.tls
	info := chosen.info
	info.Generation++

	if err := g.addBondNamedPath(ctx, info, chosen.binder, true); err != nil {
		replacement.Close()
		g.bond = old
		g.bondToken = oldToken
		g.datagrams = oldPackets
		g.cfg = oldCfg
		g.tls = oldTLS
		return err
	}
	chosen.info = info
	chosen.mux = replacement
	b.paths[info.ID] = chosen
	_ = replacement.Session.ProbeData(info.ID)
	g.datagrams = gateway.NewPacketMux(replacement)
	g.session++
	b.rotationGeneration = g.session
	if active {
		g.bondDraining = append(g.bondDraining, old)
	}
	return nil
}
func (b *runtimeGateway) SessionRotated() bool {
	b.g.mu.Lock()
	defer b.g.mu.Unlock()
	return b.rotationGeneration == b.g.session
}
