package serverexit

import (
	"context"
	"fmt"
	"os"
	"quiclab/internal/awg"
	"quiclab/internal/backup"
	"sync"
)

// LockAWGPeer is shared by legacy transit and named exits in one installation.
// A peer cannot roam between these processes, even if configured under two IDs.
func LockAWGPeer(dir string, c *awg.Config) (*os.File, error) {
	if c == nil || c.PeerIdentity() == ([32]byte{}) || privateDirectory(dir) != nil {
		return nil, ErrConfig
	}
	f, err := backup.AcquireProcessLock(dir, fmt.Sprintf(".awg-exit-peer-%x.lock", c.PeerIdentity()))
	if err != nil {
		return nil, ErrDenied
	}
	return f, nil
}

func StartOwned(ctx context.Context, c *Catalog, dir string) (*Runtime, error) {
	return startRuntime(c, func(d Definition) (Provider, error) {
		parsed, err := awg.Parse(d.AWGConfig)
		if err != nil {
			return nil, ErrConfig
		}
		lock, err := LockAWGPeer(dir, parsed)
		if err != nil {
			return nil, err
		}
		provider, err := startAWG(ctx, d)
		if err != nil {
			lock.Close()
			return nil, err
		}
		return &ownedProvider{Provider: provider, lock: lock}, nil
	})
}

type ownedProvider struct {
	Provider
	lock *os.File
	once sync.Once
}

func (p *ownedProvider) Close() { p.once.Do(func() { p.Provider.Close(); p.lock.Close() }) }
