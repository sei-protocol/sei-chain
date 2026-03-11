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
	}

	for i := 0; i < workers; i++ {
		go fp.worker()
	}

	// Shutdown the work pool when the context is done.
	go func() {
		<-ctx.Done()
		close(workQueue)

		// Handle any remaining tasks in the queue to avoid caller deadlock.
		for task := range workQueue {
			task()
		}
	}()

	return fp
}

func (fp *fixedPool) Submit(ctx context.Context, task func()) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("fixed pool is shut down")
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case fp.workQueue <- task:
		return nil
	}
}

func (fp *fixedPool) worker() {
	for task := range fp.workQueue {
		task()
	}
}
