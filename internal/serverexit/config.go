// Package serverexit defines server egress identities, independent of client
// profile/group exit IDs and of the protocol used to enter the server.
package serverexit

import (
	"errors"
	"quiclab/internal/awg"
	"quiclab/internal/exitcontrol"
	"regexp"
	"strings"
	"unicode"
)

const MaxExits = exitcontrol.MaxChoices

var ErrConfig = errors.New("invalid server exit configuration")
var ErrDenied = errors.New("server exit unavailable or not permitted")
var validID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// Definition is private server configuration. Only Metadata may be sent to clients.
type Definition struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Disabled  bool   `json:"disabled,omitempty"`
	AWGConfig string `json:"awg_config"`
}

type Metadata = exitcontrol.Metadata

// Catalog is immutable; replacing configuration never mutates a caller's snapshot.
type Catalog struct {
	definitions map[string]Definition
	order       []string
}

func NewCatalog(definitions []Definition) (*Catalog, error) {
	if len(definitions) > MaxExits {
		return nil, ErrConfig
	}
	c := &Catalog{definitions: make(map[string]Definition)}
	peers := map[[32]byte]bool{}
	for _, d := range definitions {
		if !validID.MatchString(d.ID) || d.ID == "direct" || d.ID == "legacy" || d.ID == "default" || d.Name == "" || len(d.Name) > 128 || strings.TrimSpace(d.Name) != d.Name || strings.IndexFunc(d.Name, unicode.IsControl) >= 0 || d.Protocol != "awg" {
			return nil, ErrConfig
		}
		if _, exists := c.definitions[d.ID]; exists {
			return nil, ErrConfig
		}
		parsed, err := awg.Parse(d.AWGConfig)
		if err != nil {
			return nil, ErrConfig
		}
		full := false
		for _, p := range parsed.Allowed {
			full = full || p == "0.0.0.0/0"
		}
		if !full || peers[parsed.PeerIdentity()] {
			return nil, ErrConfig
		}
		peers[parsed.PeerIdentity()] = true
		c.definitions[d.ID] = d
		c.order = append(c.order, d.ID)
	}
	return c, nil
}

func (c *Catalog) Metadata(id string) (Metadata, bool) {
	if c == nil {
		return Metadata{}, false
	}
	d, ok := c.definitions[id]
	return Metadata{ID: d.ID, Name: d.Name, Protocol: d.Protocol, Disabled: d.Disabled}, ok
}

func (c *Catalog) List() []Metadata {
	list := []Metadata{}
	if c != nil {
		for _, id := range c.order {
			m, _ := c.Metadata(id)
			list = append(list, m)
		}
	}
	return list
}
