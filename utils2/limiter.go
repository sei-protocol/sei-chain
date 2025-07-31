package utils

import (
	"context"
	"time"
)

// Limiter is a rate limiter.
// Currently implementation is very primitive.
type Limiter struct {
	rate    uint64 // permits/s
	burst   uint64 // cap on permits
	permits Watch[*uint64]
}

// NewLimiter constructs a rate limiter.
func NewLimiter(rate, burst uint64) *Limiter {
	return &Limiter{
		rate:    rate,
		burst:   burst,
		permits: NewWatch(Alloc(burst)),
	}
}

// Run runs the background tasks of the txLimiter.
func (l *Limiter) Run(ctx context.Context) error {
	const updatesPerSecond = 10
	stepTxs := l.rate / updatesPerSecond
	const stepTime = time.Second / updatesPerSecond
	for {
		if err := Sleep(ctx, stepTime); err != nil {
			return err
		}
		for w, ctrl := range l.permits.Lock() {
			*w = min(*w+stepTxs, l.burst)
			ctrl.Updated()
		}
	}
}

// Acquire acquires n permits from the rate limiter.
func (l *Limiter) Acquire(ctx context.Context, n uint64) error {
	for permits, ctrl := range l.permits.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return *permits >= n }); err != nil {
			return err
		}
		*permits -= n
		ctrl.Updated()
	}
	return nil
}
