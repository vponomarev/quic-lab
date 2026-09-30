package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/quic-go/quic-go"
	"quiclab/internal/bond"
	"quiclab/internal/bondquic"
	"sync"
	"time"
)

const BondALPN = bondquic.ALPN

type BondHello struct {
	CellDisabled bool   `json:"cell_disabled"`
	CopyBudget   uint64 `json:"copy_budget"`
	CellBudget   uint64 `json:"cell_budget"`
	Token        string `json:"token"`
	Path         string `json:"path"`
	Create       bool   `json:"create"`
}
type BondWelcome struct {
	Error string `json:"error,omitempty"`
}
type bondEntry struct {
	mux   *bond.Mux
	owner [32]byte
	ready chan struct{}
	err   error
}
type bondRegistry struct {
	mu      sync.Mutex
	entries map[string]*bondEntry
}

func (s *Server) serveBond(ctx context.Context, c *quic.Conn) {
	cs := c.ConnectionState().TLS
	if len(cs.VerifiedChains) == 0 || len(cs.PeerCertificates) == 0 || !c.ConnectionState().SupportsDatagrams.Remote {
		return
	}
	handshake, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	st, e := c.AcceptStream(handshake)
	if e != nil {
		return
	}
	defer st.Close()
	st.SetDeadline(time.Now().Add(5 * time.Second))
	var h BondHello
	if ReadJSON(st, &h) != nil {
		return
	}
	token, e := hex.DecodeString(h.Token)
	if e != nil || len(token) != 32 || (h.Path != "wifi" && h.Path != "cell") {
		WriteJSON(st, BondWelcome{Error: "invalid join"})
		return
	}
	owner := sha256.Sum256(cs.PeerCertificates[0].Raw)
	s.bonds.mu.Lock()
	if s.bonds.entries == nil {
		s.bonds.entries = map[string]*bondEntry{}
	}
	entry := s.bonds.entries[h.Token]
	fresh := false
	ownerCount := 0
	for _, v := range s.bonds.entries {
		if v.owner == owner {
			ownerCount++
		}
	}
	if entry == nil && h.Create && ownerCount < 4 && len(s.bonds.entries) < 32 {
		entry = &bondEntry{mux: bond.NewMux(ctx, false), owner: owner, ready: make(chan struct{})}
		s.bonds.entries[h.Token] = entry
		entry.mux.Session.Configure(h.CopyBudget, h.CellBudget)
		if h.CellDisabled {
			entry.mux.Session.BlockCell()
		}
		fresh = true
	}
	if entry == nil || entry.owner != owner {
		s.bonds.mu.Unlock()
		WriteJSON(st, BondWelcome{Error: "session unavailable"})
		return
	}
	s.bonds.mu.Unlock()
	if fresh {
		var release func() = func() {}
		if s.Register != nil || s.RegisterProtocol != nil {
			release, e = s.register(cs, "quic", func() { entry.mux.Close() })
		}
		if e != nil {
			entry.err = e
			entry.mux.Close()
			s.bonds.mu.Lock()
			delete(s.bonds.entries, h.Token)
			s.bonds.mu.Unlock()
			close(entry.ready)
			WriteJSON(st, BondWelcome{Error: "access denied"})
			return
		}
		count, done := s.track(cs, "QUIC / maximum availability", func() string { return c.RemoteAddr().String() })
		close(entry.ready)
		go func() {
			defer release()
			defer done()
			defer entry.mux.Close()
			defer func() { s.bonds.mu.Lock(); delete(s.bonds.entries, h.Token); s.bonds.mu.Unlock() }()
			s.serve(s.captureContext(entry.mux.Context(), cs), newDatagramMux(entry.mux), "bond-quic", func() (Stream, error) { return entry.mux.AcceptStream(entry.mux.Context()) }, func() { entry.mux.Close() }, count)
		}()
	} else {
		select {
		case <-entry.ready:
		case <-handshake.Done():
			return
		}
		if entry.err != nil {
			return
		}
	}
	if entry.mux.Context().Err() != nil {
		WriteJSON(st, BondWelcome{Error: "session expired"})
		return
	}
	// Existing per-path authorization/revocation is installed by ServeQUIC as well.
	if h.Path == "cell" && !entry.mux.Session.CellAllowed() {
		WriteJSON(st, BondWelcome{Error: "LTE session budget exhausted"})
		return
	}
	if WriteJSON(st, BondWelcome{}) != nil {
		return
	}
	if entry.mux.Session.AddPath(h.Path, bondquic.NewQUICPath(c, nil)) != nil {
		return
	}
	select {
	case <-c.Context().Done():
	case <-entry.mux.Context().Done():
	}
}

var _ Stream = (*bond.Stream)(nil)
