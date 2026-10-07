package mdm

import (
	"context"
	"time"
)

func (s *Store) WaitSync(ctx context.Context, id string, req SyncRequest, clock func() time.Time) (SyncResponse, error) {
	timer := time.NewTimer(25 * time.Second)
	defer timer.Stop()
	for {
		if e := ctx.Err(); e != nil {
			return SyncResponse{}, e
		}
		s.mu.Lock()
		wake := s.changed
		s.mu.Unlock()
		r, e := s.Sync(id, req, clock())
		if e != nil || !req.Wait || len(req.Events) > 0 || r.DesiredConfig != nil || len(r.Commands) > 0 {
			return r, e
		}
		select {
		case <-ctx.Done():
			return SyncResponse{}, ctx.Err()
		case <-timer.C:
			return r, nil
		case <-wake:
		}
	}
}
