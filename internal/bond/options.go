package bond

import (
	"context"
	"errors"
	"time"
)

type Options struct {
	DisconnectGrace                   time.Duration
	MaxPendingSession, MaxPendingFlow int
}
type PathInfo struct {
	ID, ProfileID, Network string
	Generation             uint64
}

var ErrDraining = errors.New("bond session draining")

func (o Options) normalized() Options {
	if o.DisconnectGrace <= 0 {
		o.DisconnectGrace = 120 * time.Second
	}
	if o.MaxPendingSession <= 0 || o.MaxPendingSession > 1024 {
		o.MaxPendingSession = 1024
	}
	if o.MaxPendingFlow <= 0 || o.MaxPendingFlow > 64 {
		o.MaxPendingFlow = 64
	}
	return o
}
func NewWithOptions(ctx context.Context, deliver func(Record) bool, opts Options) *Session {
	c, cancel := context.WithCancel(ctx)
	s := &Session{options: opts.normalized(), generations: map[string]uint64{}, totals: map[string]*traffic{}, recordTimeout: 30 * time.Second, ctx: c, cancel: cancel, paths: map[string]*pathState{}, pending: map[uint64]*packet{}, perFlow: map[uint32]int{}, changed: make(chan struct{}), deliver: deliver, emptySince: time.Now()}
	go s.run()
	return s
}
func (s *Session) AddNamedPath(info PathInfo, p Path) error {
	if info.Generation == 0 {
		return errors.New("path generation required")
	}
	return s.addPath(info, p)
}
