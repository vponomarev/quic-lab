package mdm

import (
	"strings"
	"time"
	"unicode"
)

func validOwner(id string) bool {
	return len(id) <= 128 && strings.IndexFunc(id, unicode.IsControl) < 0
}
func (s *Store) AssignUser(id, userID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validOwner(userID) {
		return ErrInvalid
	}
	b, ok := s.state.Bindings[id]
	if !ok {
		return ErrUnauthorized
	}
	if b.UserID == userID {
		return nil
	}
	if len(b.Audit) >= 10000 {
		return ErrLimit
	}
	n := s.clone()
	b = n.Bindings[id]
	b.UserID = userID
	b.Audit = append(b.Audit, auditEntry{Event: Event{ID: randomID(), Actor: "admin", Kind: "owner_changed", Result: userID, OccurredAt: now}, ReceivedAt: now})
	n.Bindings[id] = b
	return s.save(n)
}
func (s *Store) UnassignUser(userID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if userID == "" || !validOwner(userID) {
		return ErrInvalid
	}
	n := s.clone()
	changed := false
	for id, b := range n.Bindings {
		if b.UserID == userID {
			if len(b.Audit) >= 10000 {
				return ErrLimit
			}
			b.UserID = ""
			b.Audit = append(b.Audit, auditEntry{Event: Event{ID: randomID(), Actor: "admin", Kind: "owner_removed", OccurredAt: now}, ReceivedAt: now})
			n.Bindings[id] = b
			changed = true
		}
	}
	for id, inv := range n.Invitations {
		if inv.UserID == userID {
			inv.UserID = ""
			n.Invitations[id] = inv
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.save(n)
}
