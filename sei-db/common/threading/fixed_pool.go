package threading

import (
	"context"
	"fmt"
)

var _ Pool = (*fixedPool)(nil)

// fixedPool is a pool of workers that can be used to execute tasks concurrently.
// More efficient than spawning large numbers of short lived goroutines.
type fixedPool struct {
	workQueue chan func()
	ctx       context.Context
}

// Create a new work pool.
func NewFixedPool(
	// The work pool shuts down when the context is done.
	ctx context.Context,
	// The name of the work pool. Used for metrics.
	name string,
	// The number of workers to create.
	workers int,
	// The size of the work queue. Once full, Submit will block until a slot is available.
	queueSize int,
) Pool {

	workQueue := make(chan func(), queueSize)
	fp := &fixedPool{
		workQueue: workQueue,
		ctx:       ctx,
	}

	for i := 0; i < workers; i++ {
		go fp.worker()
	}

	go func() {
		<-ctx.Done()
		// Send a nil sentinel to each worker. Because nils are enqueued behind any
		// buffered tasks, every previously-submitted task is guaranteed to complete
		// before workers exit.
		for i := 0; i < workers; i++ {
			workQueue <- nil
		}
	}()

	return fp
}

func (fp *fixedPool) Submit(ctx context.Context, task func()) error {
	if task == nil {
		return fmt.Errorf("fixed pool: nil task")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-fp.ctx.Done():
		return fmt.Errorf("fixed pool is shut down")
	case fp.workQueue <- task:
		return nil
	}
}

func (fp *fixedPool) worker() {
	for task := range fp.workQueue {
		if task == nil {
			return
		}
		task()
	}
}
