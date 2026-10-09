package rpcadmission

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// Pool is a node-wide weighted semaphore with class-specific sub-limits.
type Pool struct {
	global   *semaphore.Weighted
	classes  map[MethodClass]*semaphore.Weighted
	timeouts ClassTimeouts
}

// NewPool returns a weighted admission pool for cfg.
func NewPool(cfg Config) (*Pool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	p := &Pool{
		classes:  make(map[MethodClass]*semaphore.Weighted),
		timeouts: cfg.ClassTimeouts,
	}
	if cfg.GlobalLimit > 0 {
		p.global = semaphore.NewWeighted(cfg.GlobalLimit)
	}
	for _, class := range allMethodClasses {
		if limit := cfg.ClassLimits.limit(class); limit > 0 {
			p.classes[class] = semaphore.NewWeighted(limit)
		}
	}
	return p, nil
}

// Permit owns capacity acquired from a Pool.
type Permit struct {
	once    sync.Once
	release func()
}

// Release returns the permit's capacity. Repeated calls are safe.
func (p *Permit) Release() {
	if p == nil || p.release == nil {
		return
	}
	p.once.Do(p.release)
}

// AcquireMethod admits one parsed RPC method name.
func (p *Pool) AcquireMethod(ctx context.Context, method string) (*Permit, error) {
	return p.Acquire(ctx, ClassifyMethod(method))
}

// Acquire admits one request in class and returns the capacity owner.
func (p *Pool) Acquire(ctx context.Context, class MethodClass) (*Permit, error) {
	if !class.valid() {
		class = ClassNormalRead
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	weight := class.Weight()
	wait := p.timeouts.timeout(class)
	acquireCtx, cancel := admissionContext(ctx, wait)
	defer cancel()

	type heldWeight struct {
		limiter *semaphore.Weighted
		weight  int64
	}
	held := make([]heldWeight, 0, 2)
	releaseHeld := func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].limiter.Release(held[i].weight)
		}
	}
	if limiter := p.classes[class]; limiter != nil {
		if err := acquire(limiter, acquireCtx, weight, wait); err != nil {
			releaseHeld()
			return nil, err
		}
		held = append(held, heldWeight{limiter, weight})
	}
	if p.global != nil {
		if err := acquire(p.global, acquireCtx, weight, wait); err != nil {
			releaseHeld()
			return nil, err
		}
		held = append(held, heldWeight{p.global, weight})
	}
	return &Permit{release: releaseHeld}, nil
}

func admissionContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func acquire(limiter *semaphore.Weighted, ctx context.Context, weight int64, timeout time.Duration) error {
	if timeout <= 0 {
		if limiter.TryAcquire(weight) {
			return nil
		}
		return context.DeadlineExceeded
	}
	return limiter.Acquire(ctx, weight)
}
