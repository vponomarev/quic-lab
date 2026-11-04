package approuting

import "strings"

// TUNPolicy is distinct from version 1 WFP identity/guard semantics. Paths are
// canonicalized again by the Windows service before use. Nil policy is legacy.
type TUNPolicy struct {
	Version int      `json:"version"`
	Enabled bool     `json:"enabled"`
	Mode    Mode     `json:"mode"`
	Only    []string `json:"only"`
	Except  []string `json:"except"`
	Unknown Decision `json:"unknown,omitempty"`
}

func (p *TUNPolicy) Clone() *TUNPolicy {
	if p == nil {
		return nil
	}
	c := *p
	c.Only = append([]string{}, p.Only...)
	c.Except = append([]string{}, p.Except...)
	return &c
}

type TUNCompiled struct {
	enabled      bool
	mode         Mode
	only, except map[string]bool
	unknown      Decision
}

func pathKey(s string) (string, bool) {
	if len(s) < 7 || len(s) > 32760 || !((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) || s[1:3] != `:\` || !strings.EqualFold(s[len(s)-4:], ".exe") {
		return "", false
	}
	for _, r := range s[3:] {
		if r < 32 || strings.ContainsRune(`/:*?"<>|`, r) {
			return "", false
		}
	}
	for _, part := range strings.Split(s[3:], `\`) {
		if part == "" || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return "", false
		}
	}
	return strings.ToLower(s), true
}

func CompileTUN(p TUNPolicy) (*TUNCompiled, error) {
	if p.Version != 2 || (p.Mode != OnlySelected && p.Mode != AllExceptSelected) {
		return nil, ErrPolicy
	}
	if p.Unknown == "" {
		p.Unknown = Tunnel
	}
	if p.Unknown != Tunnel && p.Unknown != Direct && p.Unknown != Block {
		return nil, ErrPolicy
	}
	total := 0
	list := func(paths []string) (map[string]bool, error) {
		if len(paths) > 1024 {
			return nil, ErrPolicy
		}
		m := make(map[string]bool, len(paths))
		for _, path := range paths {
			total += len(path)
			key, ok := pathKey(path)
			if !ok || m[key] || total > 256*1024 {
				return nil, ErrPolicy
			}
			m[key] = true
		}
		return m, nil
	}
	only, e := list(p.Only)
	if e != nil {
		return nil, e
	}
	except, e := list(p.Except)
	if e != nil {
		return nil, e
	}
	return &TUNCompiled{p.Enabled, p.Mode, only, except, p.Unknown}, nil
}

func (c *TUNCompiled) Decide(a Attribution) Decision {
	if c == nil {
		return Block
	}
	if !c.enabled {
		return Tunnel
	}
	if (a.Quality != Resolved && a.Quality != Shared) || len(a.Owners) == 0 || (a.Quality == Resolved && len(a.Owners) != 1) {
		return c.unknown
	}
	var agreed Decision
	for _, owner := range a.Owners {
		key, ok := pathKey(owner.Path)
		if !ok {
			return c.unknown
		}
		selected := c.only[key]
		if c.mode == AllExceptSelected {
			selected = !c.except[key]
		}
		exit := Direct
		if selected {
			exit = Tunnel
		}
		if agreed != "" && agreed != exit {
			return c.unknown
		}
		agreed = exit
	}
	return agreed
}
