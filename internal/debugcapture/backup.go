package debugcapture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"quiclab/internal/backup"
	"strings"
	"sync"
	"time"
)

type captureBackup struct {
	manager *Manager
	blocks  map[string][][]byte
}

func (m *Manager) BackupParticipant() backup.Participant { return &captureBackup{manager: m} }
func captureLock(ctx context.Context, mu *sync.Mutex) error {
	for !mu.TryLock() {
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		mu.Unlock()
		return err
	}
	return nil
}
func (p *captureBackup) Pin(ctx context.Context, kind backup.Kind, _ string) (func() error, error) {
	p.blocks = map[string][][]byte{}
	if kind != backup.Full {
		return nil, nil
	}
	m := p.manager
	if err := captureLock(ctx, &m.mu); err != nil {
		return nil, err
	}
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if id == "" || strings.ContainsAny(id, "/\\.\x00") {
			return nil, errors.New("invalid capture id")
		}
		if err := captureLock(ctx, &s.mu); err != nil {
			return nil, err
		}
		p.blocks[id] = append([][]byte(nil), s.blocks...)
		s.mu.Unlock()
	}
	return nil, nil
}
func (p *captureBackup) Transform(ctx context.Context, _ backup.Kind, dir string) error {
	defer func() { p.blocks = nil }()
	if len(p.blocks) == 0 {
		return nil
	}
	target := filepath.Join(dir, "data/downloads/capture")
	if err := os.MkdirAll(target, 0700); err != nil {
		return err
	}
	for id, blocks := range p.blocks {
		if len(blocks) == 0 {
			continue
		}
		f, err := os.OpenFile(filepath.Join(target, id+".pcapng"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		for _, b := range blocks {
			if err = ctx.Err(); err != nil {
				break
			}
			if _, err = f.Write(b); err != nil {
				break
			}
		}
		ce := f.Close()
		if err != nil {
			return err
		}
		if ce != nil {
			return ce
		}
	}
	return nil
}
