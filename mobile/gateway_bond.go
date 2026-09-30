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
	"time"
)

func (g *Gateway) startBond(binder SocketBinder) error {
	g.ctx, g.cancel = context.WithCancel(context.Background())
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
		g.cancel()
		g.cancel = nil
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
		g.cancel()
		g.cancel = nil
		return e
	}
	g.datagrams = gateway.NewPacketMux(g.bond)
	g.session++
	g.emit("connected", map[string]any{"session": g.session, "transport": "quic", "detail": "Maximum availability: independent QUIC paths"})
	go g.bondStats(g.ctx, g.bond, g.bondCellUsed, g.session)
	if g.cfg.TransitEndpoint != "" {
		go g.transitHeartbeat(g.ctx, nil, g.cfg.TransitEndpoint)
	}
	return nil
}
func (g *Gateway) addBondPath(name string, binder SocketBinder, create bool) error {
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
	if e = gateway.WriteJSON(st, gateway.BondHello{Token: g.bondToken, Path: name, Create: create, CopyBudget: g.cfg.BondCopyBudget, CellBudget: g.remainingBondBudget(), CellDisabled: g.bondCellBlocked}); e != nil {
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
	if !c.ConnectionState().SupportsDatagrams.Remote {
		return fail(errors.New("bond requires QUIC DATAGRAM"))
	}
	path := bondquic.NewQUICPath(c, cleanup)
	if e = g.bond.Session.AddPath(name, path); e != nil {
		path.Close()
		return e
	}
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
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.Context().Done():
			g.emit("disconnected", map[string]any{"detail": "Both paths failed; availability session expired"})
			return
		case <-t.C:
			if ctx.Err() != nil {
				return
			}
			v := m.Session.Stats()
			g.emit("bond_stats", map[string]any{"cell_sent": v.CellSent + offset, "cell_blocked": v.CellBlocked, "paths": v.Paths, "pending": v.Pending, "oldest_ms": v.OldestMS, "rescued": v.Rescued, "duplicates": v.Duplicates, "expired": v.Expired})
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
