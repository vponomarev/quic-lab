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

func StartOwned(ctx context.Context, c *Catalog, dir string, legacyDirs ...string) (*Runtime, error) {
	// Legacy transit may keep its probe socket outside DataDir. Own its lock
	// there as well; canonical aliases must not cause us to lock ourselves out.
	dirs := []string{}
	infos := []os.FileInfo{}
	for _, path := range append([]string{dir}, legacyDirs...) {
		info, err := os.Stat(path)
		if err != nil || privateDirectory(path) != nil {
			return nil, ErrConfig
		}
		duplicate := false
		for _, previous := range infos {
			duplicate = duplicate || os.SameFile(info, previous)
		}
		if !duplicate {
			dirs = append(dirs, path)
			infos = append(infos, info)
		}
	}
	return startRuntime(c, func(d Definition) (Provider, error) {
		parsed, err := awg.Parse(d.AWGConfig)
		if err != nil {
			return nil, ErrConfig
		}
		locks := []*os.File{}
		unlock := func() {
			for _, lock := range locks {
				lock.Close()
			}
		}
		for _, path := range dirs {
			lock, err := LockAWGPeer(path, parsed)
			if err != nil {
				unlock()
				return nil, err
			}
			locks = append(locks, lock)
		}
		provider, err := startAWG(ctx, d)
		if err != nil {
			unlock()
			return nil, err
		}
		return &ownedProvider{Provider: provider, locks: locks}, nil
	})
}

type ownedProvider struct {
	Provider
	locks []*os.File
	once  sync.Once
}

func (p *ownedProvider) Close() {
	p.once.Do(func() {
		p.Provider.Close()
		for _, lock := range p.locks {
			lock.Close()
		}
	})
}
