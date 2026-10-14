// Package approuting defines selection semantics; it does not install filters or route traffic.
package approuting

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

type Mode string

const (
	OnlySelected      Mode = "only_selected"
	AllExceptSelected Mode = "all_except_selected"
)

type State string

const (
	Stopped  State = "stopped"
	Starting State = "starting"
	Ready    State = "ready"
	Failed   State = "failed"
)

type Decision string

const (
	Direct Decision = "direct"
	Tunnel Decision = "tunnel"
	Block  Decision = "block"
)

type Policy struct {
	Version int      `json:"version"`
	Mode    Mode     `json:"mode"`
	Only    []string `json:"only"`
	Except  []string `json:"except"`
}

var ErrPolicy = errors.New("invalid application routing policy")

type Compiled struct {
	mode                    Mode
	only, except, protected map[string]bool
}

func validID(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func idSet(ids []string) (map[string]bool, error) {
	if len(ids) > 1024 {
		return nil, ErrPolicy
	}
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !validID(id) || m[id] {
			return nil, ErrPolicy
		}
		m[id] = true
	}
	return m, nil
}
func Compile(p Policy, protectedIDs []string) (*Compiled, error) {
	if p.Version != 1 || (p.Mode != OnlySelected && p.Mode != AllExceptSelected) {
		return nil, ErrPolicy
	}
	only, e := idSet(p.Only)
	if e != nil {
		return nil, e
	}
	except, e := idSet(p.Except)
	if e != nil {
		return nil, e
	}
	protected, e := idSet(protectedIDs)
	if e != nil {
		return nil, e
	}
	for id := range protected {
		if only[id] {
			return nil, ErrPolicy
		}
	}
	return &Compiled{p.Mode, only, except, protected}, nil
}

// IDs must be resolved from authoritative WFP AppIDs, never caller-provided process names.
// Ready means the platform has installed guards and verified the tunnel, not merely started a service.
func (c *Compiled) Decide(id, protocol string, ipv6 bool, state State) Decision {
	if state == Stopped {
		return Direct
	}
	if c == nil || (c.mode != OnlySelected && c.mode != AllExceptSelected) || !validID(id) {
		return Block
	}
	if c.protected[id] {
		return Direct
	}
	selected := c.only[id]
	if c.mode == AllExceptSelected {
		selected = !c.except[id]
	}
	if !selected {
		return Direct
	}
	if state != Ready || ipv6 || (protocol != "tcp" && protocol != "udp") {
		return Block
	}
	return Tunnel
}
func Parse(raw []byte) (Policy, error) {
	if len(raw) > 1<<20 {
		return Policy{}, ErrPolicy
	}
	if !uniqueFields(raw) {
		return Policy{}, ErrPolicy
	}
	var p Policy
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return Policy{}, ErrPolicy
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Policy{}, ErrPolicy
	}
	if _, e := Compile(p, nil); e != nil {
		return Policy{}, e
	}
	return p, nil
}

// Keep ID text canonical for JSON; executable display names never influence matching.
func IsCanonicalID(id string) bool { return validID(id) }

// Reject duplicate and case-aliased keys rather than silently accepting the last value.
func uniqueFields(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for d.More() {
		token, e = d.Token()
		if e != nil {
			return false
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return false
		}
		switch key {
		case "version", "mode", "only", "except":
		default:
			return false
		}
		seen[key] = true
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return false
		}
	}
	token, e = d.Token()
	return e == nil && token == json.Delim('}')
}
