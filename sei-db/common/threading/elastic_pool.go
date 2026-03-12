package threading

import (
	"context"
	"fmt"
)

var _ Pool = (*elasticPool)(nil)

// elasticPool is a pool that guarantees every submitted task begins executing
// immediately without waiting for other tasks to finish first. It maintains a
// set of warm workers for goroutine reuse, and spawns temporary goroutines when
// all warm workers are busy.
//
// This is useful when tasks submitted to the pool may depend on other tasks in
// the same pool. For example, if task A is submitted and then submits task B,
// and A waits for B to complete, a fixed-size pool may deadlock when all
// workers are occupied, since task B can never be scheduled. An
// elastic pool avoids this by ensuring B starts immediately in a temporary
// goroutine if all workers are busy.
type elasticPool struct {
	workQueue chan func()
	ctx       context.Context
}

// NewElasticPool creates a pool with the given number of warm workers. Submitted
// tasks are handed off to an idle warm worker if one is available, otherwise a
// temporary goroutine is spawned. Tasks are never queued behind other tasks.
func NewElasticPool(
	ctx context.Context,
	name string,
	warmWorkers int,
) Pool {
	workQueue := make(chan func())
	ep := &elasticPool{
		workQueue: workQueue,
		ctx:       ctx,
	}

	for i := 0; i < warmWorkers; i++ {
		go ep.worker()
	}

	go func() {
		<-ctx.Done()
		for i := 0; i < warmWorkers; i++ {
			workQueue <- nil
		}
	}()

	return ep
}

func (ep *elasticPool) Submit(ctx context.Context, task func()) error {
	if task == nil {
		return fmt.Errorf("elastic pool: nil task")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ep.ctx.Done():
		return fmt.Errorf("elastic pool is shut down")
	case ep.workQueue <- task:
		return nil
	default:
		// All warm workers are busy; spawn a temporary goroutine.
		go task()
		return nil
	}
}

func (ep *elasticPool) worker() {
	for task := range ep.workQueue {
		if task == nil {
			return
		}
		task()
	}
}
