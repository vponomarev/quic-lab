package bond

// PreferPath records the runtime's stable profile selection. A penalized or
// unavailable preferred path still allows the ordinary hedge/failover scheduler.
func (s *Session) PreferPath(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paths[id] != nil {
		s.preferredPath = id
	}
}
