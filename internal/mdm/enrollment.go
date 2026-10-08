package mdm

import (
	"time"
)

func validRights(r Rights) bool {
	return r.LANMode == "" || r.LANMode == "deny" || r.LANMode == "confirm" || r.LANMode == "allow"
}
func (s *Store) CreateInvitation(now time.Time, rights Rights) (Invitation, error) {
	return s.CreateUserInvitation(now, rights, "")
}
func (s *Store) CreateUserInvitation(now time.Time, rights Rights, userID string) (Invitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validRights(rights) || !validOwner(userID) {
		return Invitation{}, ErrInvalid
	}
	n := s.clone()
	if len(n.Invitations) >= 10000 {
		return Invitation{}, ErrLimit
	}
	inv := Invitation{Token: randomID(), ExpiresAt: now.Add(24 * time.Hour), RequestedRights: rights}
	n.Invitations[digest(inv.Token)] = invitationState{Expires: inv.ExpiresAt, Rights: rights, UserID: userID}
	return inv, s.save(n)
}
func (s *Store) Redeem(req EnrollmentRequest, now time.Time) (Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Version != 1 || !validSecret(req.Secret) || !validSecret(req.Token) || len(req.RegistrationID) == 0 || len(req.RegistrationID) > 128 {
		return Binding{}, ErrInvalid
	}
	key := digest(req.Token)
	inv, ok := s.state.Invitations[key]
	if !ok {
		return Binding{}, ErrUnauthorized
	}
	if inv.BindingID != "" {
		b := s.state.Bindings[inv.BindingID]
		if inv.RegistrationID != req.RegistrationID || b.SecretHash != digest(req.Secret) {
			return Binding{}, ErrUnauthorized
		}
		return b.Binding, nil
	}
	if !now.Before(inv.Expires) {
		return Binding{}, ErrUnauthorized
	}
	if len(s.state.Bindings) >= 10000 {
		return Binding{}, ErrLimit
	}
	// A secret must identify exactly one binding.
	for _, b := range s.state.Bindings {
		if b.SecretHash == digest(req.Secret) {
			return Binding{}, ErrConflict
		}
	}
	n := s.clone()
	b := Binding{ID: randomID(), Epoch: 0, RequestedRights: inv.Rights}
	n.Bindings[b.ID] = bindingState{UserID: inv.UserID, Binding: b, SecretHash: digest(req.Secret), Seen: map[string]time.Time{}}
	inv.BindingID = b.ID
	inv.RegistrationID = req.RegistrationID
	n.Invitations[key] = inv
	return b, s.save(n)
}
