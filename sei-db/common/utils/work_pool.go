package utils

import (
	"context"
	"fmt"
)

// WorkPool is a pool of workers that can be used to execute tasks concurrently.
// More efficient than spawning large numbers of short lived goroutines.
type WorkPool struct {
	ctx       context.Context
	workQueue chan func()
}

// TODO add metrics!
// TODO unit test before merging!

// Create a new work pool.
func NewWorkPool(
	// The work pool shuts down when the context is done.
	ctx context.Context,
	// The name of the work pool. Used for metrics.
	name string,
	// The number of workers to create.
	workers int,
	// The size of the work queue. Once full, Submit will block until a slot is available.
	queueSize int,
) *WorkPool {

	workQueue := make(chan func(), queueSize)
	workPool := &WorkPool{
		ctx:       ctx,
		workQueue: workQueue,
	}

	for i := 0; i < workers; i++ {
		go workPool.worker()
	}

	// Shutdown the work pool when the context is done.
	go func() {
		<-ctx.Done()
		close(workQueue)
	}()

	return workPool
}

// Submit submits a task to the work pool. This method does not block until the task is executed.
//
// If wp is nil, the task is executed asynchronously in a one-off goroutine.
func (wp *WorkPool) Submit(ctx context.Context, task func()) (err error) {
	if wp == nil {
		go task()
		return nil
	}

	defer func() {
		if recover() != nil {
			err = fmt.Errorf("work pool is shut down")
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wp.ctx.Done():
		return fmt.Errorf("work pool is shut down")
	case wp.workQueue <- task:
		return nil
	}
}

func (wp *WorkPool) worker() {
	for task := range wp.workQueue {
		task()
	}
}
