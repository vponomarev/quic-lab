package mobile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/quic-go/quic-go"
	"net"
	"quiclab/internal/bond"
	"quiclab/internal/bondquic"
	"quiclab/internal/gateway"
	"quiclab/internal/pathpolicy"
	"time"
)

func (g *Gateway) startBond(binder SocketBinder) error {
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
	if g.cfg.Transport == "https" {
		g.bondPathGeneration++
		return g.addBondHTTPS(g.ctx, g.cfg.Endpoint, bond.PathInfo{ID: name, ProfileID: "legacy", Network: name, Generation: g.bondPathGeneration}, binder, create)
	}
	host, _, _ := net.SplitHostPort(g.cfg.Endpoint)
	tr, e := openTransport(net.ParseIP(host), binder, nil)
	if e != nil {
		return e
	}
	cleanup := func() { tr.q.Close(); tr.udp.Close() }
	tc := g.tls.Clone()
	tc.NextProtos = []string{bondquic.ALPN}
	ctx, cancel := context.WithTimeout(g.ctx, 3*time.Second)
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
	st.SetDeadline(time.Now().Add(3 * time.Second))
	g.bondPathGeneration++
	info := bond.PathInfo{ID: name, ProfileID: "legacy", Network: name, Generation: g.bondPathGeneration}
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
	g.bondBinders[name] = binder
	return nil
}

// EnsureBondPath reconnects a missing path without replacing the shared session.
func (g *Gateway) EnsureBondPath(name string, binder SocketBinder) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.bond == nil {
		return errors.New("maximum availability is not running")
	}
	if name == "cell" && !g.bond.Session.CellAllowed() {
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
			e = d.g.rotateBondLocked(m)
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
	return !g.bondCellBlocked && (g.bond == nil || g.bond.Session.CellAllowed())
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
func (g *Gateway) rotateBondLocked(old *bond.Mux) error {
	// C1 must share the LTE ledger across current and draining sessions first.
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
