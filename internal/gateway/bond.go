package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"github.com/quic-go/quic-go"
	"quiclab/internal/bond"
	"quiclab/internal/bondquic"
	"sync"
	"time"
)

const BondALPN = bondquic.ALPN

type BondHello struct {
	PathID       string `json:"path_id,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	Network      string `json:"network,omitempty"`
	Generation   uint64 `json:"generation,omitempty"`
	CellDisabled bool   `json:"cell_disabled"`
	CopyBudget   uint64 `json:"copy_budget"`
	CellBudget   uint64 `json:"cell_budget"`
	Token        string `json:"token"`
	Path         string `json:"path"`
	Create       bool   `json:"create"`
}
type BondWelcome struct {
	Token string `json:"token,omitempty"`
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

// joinBond authenticates session ownership independently of the transport.
// root must outlive any one path; handshake may be canceled with its request.
func (s *Server) joinBond(root, handshake context.Context, cs tls.ConnectionState, h BondHello, protocol, remote string) (*bondEntry, string, error) {
	if len(cs.VerifiedChains) == 0 || len(cs.PeerCertificates) == 0 {
		return nil, "", errors.New("client certificate required")
	}
	token, e := hex.DecodeString(h.Token)
	if e != nil || len(token) != 32 || (h.PathID == "" && h.Path != "wifi" && h.Path != "cell") {
		return nil, "", errors.New("invalid join")
	}
	if h.Create {
		var secret [32]byte
		if _, e = rand.Read(secret[:]); e != nil {
			return nil, "", e
		}
		h.Token = hex.EncodeToString(secret[:])
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
		entry = &bondEntry{mux: bond.NewMuxWithOptions(root, false, s.BondOptions), owner: owner, ready: make(chan struct{})}
		s.bonds.entries[h.Token] = entry
		entry.mux.Session.Configure(h.CopyBudget, h.CellBudget)
		if h.CellDisabled {
			entry.mux.Session.BlockCell()
		}
		fresh = true
	}
	if entry == nil || entry.owner != owner {
		s.bonds.mu.Unlock()
		return nil, "", errors.New("session unavailable")
	}
	s.bonds.mu.Unlock()
	if fresh {
		release := func() {}
		if s.Register != nil || s.RegisterProtocol != nil {
			release, e = s.register(cs, protocol, func() { entry.mux.Close() })
		}
		if e != nil {
			entry.err = e
			entry.mux.Close()
			s.bonds.mu.Lock()
			delete(s.bonds.entries, h.Token)
			s.bonds.mu.Unlock()
			close(entry.ready)
			return nil, "", errors.New("access denied")
		}
		count, done := s.track(cs, "Bond / "+protocol, func() string { return remote })
		close(entry.ready)
		go func() {
			defer release()
			defer done()
			defer entry.mux.Close()
			defer func() { s.bonds.mu.Lock(); delete(s.bonds.entries, h.Token); s.bonds.mu.Unlock() }()
			s.serve(s.captureContext(entry.mux.Context(), cs), newDatagramMux(entry.mux), "bond", func() (Stream, error) { return entry.mux.AcceptStream(entry.mux.Context()) }, func() { entry.mux.Close() }, count)
		}()
	} else {
		select {
		case <-entry.ready:
		case <-handshake.Done():
			return nil, "", handshake.Err()
		}
		if entry.err != nil {
			return nil, "", entry.err
		}
	}
	if entry.mux.Context().Err() != nil {
		return nil, "", errors.New("session expired")
	}
	if (h.Network == "cell" || h.Path == "cell") && !entry.mux.Session.CellAllowed() {
		return nil, "", errors.New("LTE session budget exhausted")
	}
	return entry, h.Token, nil
}
func attachBond(entry *bondEntry, h BondHello, path bond.Path) error {
	if h.PathID != "" {
		return entry.mux.Session.AddNamedPath(bond.PathInfo{ID: h.PathID, ProfileID: h.ProfileID, Network: h.Network, Generation: h.Generation}, path)
	}
	return entry.mux.Session.AddPath(h.Path, path)
}
func (s *Server) serveBond(ctx context.Context, c *quic.Conn) {
	if !c.ConnectionState().SupportsDatagrams.Remote {
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
	entry, token, e := s.joinBond(ctx, handshake, c.ConnectionState().TLS, h, "quic", c.RemoteAddr().String())
	if e != nil {
		WriteJSON(st, BondWelcome{Error: e.Error()})
		return
	}
	if WriteJSON(st, BondWelcome{Token: token}) != nil {
		return
	}
	path := bondquic.NewQUICPath(c, nil)
	defer path.Close()
	if attachBond(entry, h, path) != nil {
		return
	}
	select {
	case <-c.Context().Done():
	case <-entry.mux.Context().Done():
	}
}

var _ Stream = (*bond.Stream)(nil)
