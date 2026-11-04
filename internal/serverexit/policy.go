package serverexit

// Nil policy preserves the legacy server exit. An explicit empty policy denies
// egress; it is not equivalent to inheritance or permission to use direct.
type Policy struct {
	Allowed []string `json:"allowed"`
	Default string   `json:"default,omitempty"`
}

func (p *Policy) Clone() *Policy {
	if p == nil {
		return nil
	}
	return &Policy{Allowed: append([]string{}, p.Allowed...), Default: p.Default}
}

func (c *Catalog) ValidatePolicy(p *Policy) error {
	if p == nil {
		return nil
	}
	if len(p.Allowed) > MaxExits {
		return ErrConfig
	}
	seen := map[string]bool{}
	for _, id := range p.Allowed {
		if _, exists := c.Metadata(id); !exists || seen[id] {
			return ErrConfig
		}
		seen[id] = true
	}
	if p.Default != "" && !seen[p.Default] {
		return ErrConfig
	}
	return nil
}

// Resolve never replaces an explicit invalid/disallowed choice with the default.
// Empty result without error is reserved for unchanged legacy policy.
func (c *Catalog) Resolve(p *Policy, selected string) (string, error) {
	if p == nil {
		if selected == "" {
			return "", nil
		}
		return "", ErrDenied
	}
	if len(p.Allowed) > MaxExits {
		return "", ErrDenied
	}
	// A removed catalogue entry must not invalidate another explicitly allowed
	// choice. Validate shape here; catalogue membership is checked for the actual ID.
	seen := map[string]bool{}
	for _, id := range p.Allowed {
		if !validID.MatchString(id) || seen[id] {
			return "", ErrDenied
		}
		seen[id] = true
	}
	id := selected
	if id == "" {
		id = p.Default
	}
	allowed := false
	for _, permitted := range p.Allowed {
		allowed = allowed || id == permitted
	}
	m, exists := c.Metadata(id)
	if !allowed || !exists || m.Disabled {
		return "", ErrDenied
	}
	return id, nil
}
