package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Participant interface {
	Pin(context.Context, Kind, string) (func() error, error)
}
type preparer interface {
	Prepare(context.Context, Kind) error
}
type transformer interface {
	Transform(context.Context, Kind, string) error
}
type Coordinator struct {
	root         string
	participants []Participant
	version      string
	busy         chan struct{}
}

func NewCoordinator(root string, participants []Participant, serverVersion string) *Coordinator {
	return &Coordinator{root: root, participants: participants, version: serverVersion, busy: make(chan struct{}, 1)}
}
func (c *Coordinator) Capture(ctx context.Context, kind Kind) (snapshot *Snapshot, err error) {
	if kind != Config && kind != Full {
		return nil, errors.New("invalid backup type")
	}
	select {
	case c.busy <- struct{}{}:
		defer func() { <-c.busy }()
	default:
		return nil, errors.New("snapshot busy")
	}
	var dir string
	defer func() {
		if err != nil && dir != "" {
			os.RemoveAll(dir)
		}
	}()
	for attempt := 0; attempt < 3; attempt++ {
		epoch, e := PublicationEpoch(c.root)
		if e != nil {
			return nil, e
		}
		for _, p := range c.participants {
			if pre, ok := p.(preparer); ok {
				if e = pre.Prepare(ctx, kind); e != nil {
					return nil, e
				}
			}
		}
		dir, e = os.MkdirTemp(c.root, ".backup-snapshot-")
		if e != nil {
			return nil, e
		}
		barrier, cancel := context.WithTimeout(ctx, time.Second)
		unlock, e := FreezePublication(barrier, c.root)
		if e != nil {
			cancel()
			return nil, e
		}
		nowEpoch, e := PublicationEpoch(c.root)
		if e != nil || nowEpoch != epoch {
			unlock()
			cancel()
			os.RemoveAll(dir)
			dir = ""
			if e != nil {
				return nil, e
			}
			continue
		}
		releases := []func() error{}
		var pinErr error
		for _, p := range c.participants {
			if pinErr = barrier.Err(); pinErr != nil {
				break
			}
			release, e := p.Pin(barrier, kind, dir)
			if release != nil {
				releases = append(releases, release)
			}
			if e != nil {
				pinErr = e
				break
			}
		}
		if pinErr == nil {
			pinErr = barrier.Err()
		}
		unlock()
		cancel()
		for i := len(releases) - 1; i >= 0; i-- {
			if e = releases[i](); pinErr == nil {
				pinErr = e
			}
		}
		if pinErr != nil {
			return nil, pinErr
		}
		for _, p := range c.participants {
			if tr, ok := p.(transformer); ok {
				if e = tr.Transform(ctx, kind, dir); e != nil {
					return nil, e
				}
			}
		}
		m := Manifest{FormatVersion: 1, Kind: kind, CreatedAt: time.Now().UTC(), ServerVersion: c.version}
		components := map[string]bool{}
		e = filepath.WalkDir(dir, func(name string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() {
				return errors.New("non-regular snapshot entry")
			}
			rel, e := filepath.Rel(dir, name)
			if e != nil {
				return e
			}
			rel = filepath.ToSlash(rel)
			if !safePath(rel) {
				return errors.New("invalid snapshot path")
			}
			f, e := os.Open(name)
			if e != nil {
				return e
			}
			h := sha256.New()
			n, e := io.Copy(h, contextReader{ctx, f})
			f.Close()
			if e != nil {
				return e
			}
			m.Entries = append(m.Entries, Entry{Path: rel, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))})
			components[strings.SplitN(rel, "/", 2)[0]] = true
			return nil
		})
		if e != nil {
			return nil, e
		}
		for component := range components {
			m.Components = append(m.Components, component)
		}
		sort.Strings(m.Components)
		if e = validate(m, Limits{100000, 64 << 30}); e != nil {
			return nil, e
		}
		completed := dir
		return &Snapshot{Dir: dir, Manifest: m, Release: func() error { return os.RemoveAll(completed) }}, nil
	}
	return nil, errors.New("state changed during backup preparation; retry")
}

// FileParticipant enumerates before the barrier. Atomic replacement and deletion
// of every included source must participate in Publish; existing inodes must never
// be edited in place. Enumeration races are detected by the publication epoch.
type FileParticipant struct {
	Root, Prefix string
	Required     []string
	Include      func(string, Kind) bool
	files        []string
}

func (p *FileParticipant) Prepare(ctx context.Context, kind Kind) error {
	p.files = nil
	seen := map[string]bool{}
	// systemd exposes StateDirectory through one top-level symlink. Resolve
	// that configured root only; links inside the inventory remain rejected.
	walkRoot, err := filepath.EvalSymlinks(p.Root)
	if err != nil {
		return err
	}
	err = filepath.WalkDir(walkRoot, func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, e := filepath.Rel(walkRoot, name)
		if e != nil {
			return e
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "backups" {
				return filepath.SkipDir
			}
			return nil
		}
		if p.Include == nil || !p.Include(rel, kind) {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return errors.New("unsafe snapshot source")
		}
		p.files = append(p.files, rel)
		seen[rel] = true
		if len(p.files) > 100000 {
			return errors.New("snapshot file limit")
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, name := range p.Required {
		if !seen[name] {
			return errors.New("required backup source unavailable")
		}
	}
	return nil
}
func (p *FileParticipant) Pin(ctx context.Context, _ Kind, dir string) (func() error, error) {
	if p.Prefix != "" && !safePath(p.Prefix) {
		return nil, errors.New("invalid snapshot prefix")
	}
	for _, rel := range p.files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !safePath(rel) {
			return nil, errors.New("invalid snapshot source")
		}
		dst := filepath.Join(dir, p.Prefix, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return nil, err
		}
		if err := os.Link(filepath.Join(p.Root, filepath.FromSlash(rel)), dst); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
