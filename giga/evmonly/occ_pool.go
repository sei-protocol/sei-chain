package evmonly

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

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
	return &occWorkerPool{workers: max(workers, 1)}
}

func (p *occWorkerPool) Run(ctx context.Context, workItems int, run func(context.Context, int, int) error) error {
	workers := p.workers
	if workItems > 0 {
		workers = min(workers, workItems)
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

// RunStep runs like Run and records the run's duration, and its longest worker's, under step.
func (p *occWorkerPool) RunStep(ctx context.Context, step occStep, workItems int, run func(context.Context, int, int) error) error {
	start := time.Now()
	var longest atomic.Int64
	err := p.Run(ctx, workItems, func(workerCtx context.Context, workerID int, workers int) error {
		workerStart := time.Now()
		defer func() { raiseTo(&longest, int64(time.Since(workerStart))) }()
		return run(workerCtx, workerID, workers)
	})
	step.record(ctx, time.Since(start), time.Duration(longest.Load()))
	return err
}

// raiseTo raises value to at least candidate.
func raiseTo(value *atomic.Int64, candidate int64) {
	for {
		current := value.Load()
		if candidate <= current || value.CompareAndSwap(current, candidate) {
			return
		}
	}
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
