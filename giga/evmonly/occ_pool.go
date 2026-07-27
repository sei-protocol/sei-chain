package evmonly

import (
	"context"
	"errors"
	"sync"

	"golang.org/x/sync/errgroup"
)

type occWorkerPool struct {
	workers int
	mu      sync.RWMutex
	closed  bool
	wg      sync.WaitGroup
}

var errOCCWorkerPoolClosed = errors.New("OCC worker pool is closed")

func newOCCWorkerPool(workers int) *occWorkerPool {
	if workers <= 0 {
		workers = 1
	}
	return &occWorkerPool{workers: workers}
}

func (p *occWorkerPool) Run(ctx context.Context, workItems int, run func(context.Context, int, int) error) error {
	workers := p.workers
	if workItems > 0 && workers > workItems {
		workers = workItems
	}
	if workers <= 0 {
		workers = 1
	}

	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return errOCCWorkerPoolClosed
	}
	p.wg.Add(1)
	p.mu.RUnlock()
	defer p.wg.Done()

	g, groupCtx := errgroup.WithContext(ctx)
	for workerID := 0; workerID < workers; workerID++ {
		workerID := workerID
		g.Go(func() error {
			if err := groupCtx.Err(); err != nil {
				return err
			}
			return run(groupCtx, workerID, workers)
		})
	}
	return g.Wait()
}

// Close rejects future runs and waits for admitted in-flight runs to drain.
func (p *occWorkerPool) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.wg.Wait()
}
