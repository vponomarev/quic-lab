package vless

import (
	"context"
	"time"
)

func (d *deviceDispatcher) lease(ctx context.Context, id string) (time.Duration, error) {
	start := time.Now()
	request, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	ttl, e := d.admission(request, id)
	if e != nil || ttl <= 0 || ttl > 2*time.Second || request.Err() != nil {
		return 0, ErrDeviceRevoked
	}
	remaining := time.Until(start.Add(ttl))
	if remaining <= 0 {
		return 0, ErrDeviceRevoked
	}
	return remaining, nil
}
func (d *deviceDispatcher) startLease(ctx context.Context, id string, f *deviceFlow) error {
	ttl, e := d.lease(ctx, id)
	if e != nil {
		return e
	}
	expiry := time.AfterFunc(ttl, f.close)
	go func() {
		defer expiry.Stop()
		for {
			renew := time.NewTimer(ttl / 2)
			select {
			case <-ctx.Done():
				renew.Stop()
				return
			case <-renew.C:
			}
			ttl, e = d.lease(ctx, id)
			if e != nil {
				f.close()
				return
			}
			if ctx.Err() != nil {
				return
			}
			// If the previous deadline already fired, f.close is irreversible.
			expiry.Reset(ttl)
		}
	}()
	return nil
}
