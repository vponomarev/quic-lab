package main

import (
	"errors"
	"quiclab/internal/research"
	"sync"
)

// researchStoreManager permits first-time configuration without a restart.
// Once opened, storage limits remain fixed until the next server start.
type researchStoreManager struct {
	mu      sync.Mutex
	options func() (research.StoreOptions, error)
	store   *research.Store
	closed  bool
}

func newResearchStoreManager(options func() (research.StoreOptions, error)) *researchStoreManager {
	return &researchStoreManager{options: options}
}
func (m *researchStoreManager) Open() (*research.Store, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("research storage is closed")
	}
	if m.store != nil {
		return m.store, nil
	}
	opts, e := m.options()
	if e != nil {
		return nil, e
	}
	s, e := research.OpenStore(opts)
	if e != nil {
		return nil, e
	}
	m.store = s
	return s, nil
}
func (m *researchStoreManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	if m.store != nil {
		return m.store.Close()
	}
	return nil
}
