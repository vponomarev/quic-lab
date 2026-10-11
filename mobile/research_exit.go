package mobile

import (
	"errors"
	"quiclab/internal/research"
)

var errInvalidResearchExit = errors.New("invalid research exit")

type researchExitRef = research.ExitRef

// SetResearchExit labels new flows only; an existing flow never changes its exit.
func (g *Gateway) SetResearchExit(id, kind string) error {
	e := research.ExitRef{Route: "vpn", ID: id, Type: kind}
	if kind == "direct" {
		e.Route = "direct"
	}
	if id == "" || len(id) > 256 || (kind != "quic" && kind != "https" && kind != "direct") || ((id == "direct") != (kind == "direct")) {
		return errInvalidResearchExit
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.researchExit = e
	if g.research != nil {
		g.research.mu.Lock()
		g.research.exit = e
		g.research.mu.Unlock()
	}
	return nil
}
