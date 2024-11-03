package clock

import (
	"context"
	"time"
)

const WALLCLOCK_PRECISION = 1 * time.Second

func AfterWallClock(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	deadline := time.Now().Add(d).Truncate(0)
	after_ch := time.After(d)
	ticker := time.NewTicker(WALLCLOCK_PRECISION)
	go func() {
		var t time.Time
		defer ticker.Stop()
		for {
			select {
			case t = <-after_ch:
				ch <- t
				return
			case t = <-ticker.C:
				if t.After(deadline) {
					ch <- t
					return
				}
			}
		}
	}()
	return ch
}

func RunTicker(ctx context.Context, interval, retryInterval time.Duration, cb func(context.Context) error) {
	go func() {
		var err error
		for {
			nextInterval := interval
			if err != nil {
				nextInterval = retryInterval
			}
			select {
			case <-ctx.Done():
				return
			case <-AfterWallClock(nextInterval):
				err = cb(ctx)
			}
		}
	}()
}
