package lakesim

import (
	"context"
	"time"
)

// throttle spaces blocks to a maximum rate. A block that starts late does not let later blocks start early.
type throttle struct {
	// The minimum time between the starts of consecutive blocks. 0 means no limit.
	interval time.Duration

	// When the next block may start.
	next time.Time
}

// newThrottle returns a throttle allowing blocksPerSecond blocks per second, or any number if it is 0.
func newThrottle(blocksPerSecond float64) *throttle {
	var interval time.Duration
	if blocksPerSecond > 0 {
		interval = time.Duration(float64(time.Second) / blocksPerSecond)
	}
	return &throttle{interval: interval, next: time.Now()}
}

// Wait waits until the next block may start. It returns false if ctx ended first.
func (t *throttle) Wait(ctx context.Context) bool {
	if t.interval == 0 {
		return ctx.Err() == nil
	}
	if delay := time.Until(t.next); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
		}
	}
	t.next = t.next.Add(t.interval)
	if now := time.Now(); t.next.Before(now) {
		// A late block must not let the blocks after it catch up in a burst.
		t.next = now
	}
	return ctx.Err() == nil
}
