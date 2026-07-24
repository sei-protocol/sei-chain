package evmonly

import (
	"context"
	"errors"
	"sync"

	"golang.org/x/sync/errgroup"
)

type occWorkerPool struct {
	workers int
	mu      sync.Mutex
	closed  bool
}

var errOCCWorkerPoolClosed = errors.New("OCC worker pool is closed")

func newOCCWorkerPool(workers int) *occWorkerPool {
	if workers <= 0 {
		workers = 1
	}
	return &occWorkerPool{workers: workers}
}

func (p *occWorkerPool) Run(ctx context.Context, ranges []occTxRange, run func(context.Context, occTxRange) error) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errOCCWorkerPoolClosed
	}
	defer p.mu.Unlock()

	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(p.workers)
	for _, txRange := range ranges {
		txRange := txRange
		g.Go(func() error {
			if err := groupCtx.Err(); err != nil {
				return err
			}
			return run(groupCtx, txRange)
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	return ctx.Err()
}

func (p *occWorkerPool) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
}
