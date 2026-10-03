package pathpolicy

import (
	"errors"
	"fmt"
	"sync"
)

// Pool reserves transport slots across all physical networks of each profile.
// Lease identities are unique, so stale cancellation cannot free a replacement.
type Pool struct {
	mu     sync.Mutex
	limit  int
	next   uint64
	slots  map[string][]string
	owners map[string]string
}

func NewPool(limit int) (*Pool, error) {
	if limit < 1 || limit > 5 {
		return nil, errors.New("pool size must be one, or two to five")
	}
	return &Pool{limit: limit, slots: map[string][]string{}, owners: map[string]string{}}, nil
}
func (p *Pool) Reserve(profileID, network string) (string, error) {
	if profileID == "" || (network != "wifi" && network != "cell") {
		return "", errors.New("invalid pool reservation")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	slots := p.slots[profileID]
	if slots == nil {
		slots = make([]string, p.limit)
		p.slots[profileID] = slots
	}
	for i, v := range slots {
		if v == "" {
			p.next++
			id := fmt.Sprintf("%s/%d", profileID, p.next)
			slots[i] = id
			p.owners[id] = profileID
			return id, nil
		}
	}
	return "", errors.New("profile pool full")
}
func (p *Pool) Release(connectionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	profileID, ok := p.owners[connectionID]
	if !ok {
		return
	}
	delete(p.owners, connectionID)
	for i, id := range p.slots[profileID] {
		if id == connectionID {
			p.slots[profileID][i] = ""
			break
		}
	}
}
