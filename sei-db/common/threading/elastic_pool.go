package threading

import "sync"

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
	wg        sync.WaitGroup
	closeOnce sync.Once
	closed    bool
}

// NewElasticPool creates a pool with the given number of warm workers. Submitted
// tasks are handed off to an idle warm worker if one is available, otherwise a
// temporary goroutine is spawned. Tasks are never queued behind other tasks.
func NewElasticPool(
	name string,
	warmWorkers int,
) Pool {
	workQueue := make(chan func())
	ep := &elasticPool{
		workQueue: workQueue,
	}

	ep.wg.Add(warmWorkers)
	for i := 0; i < warmWorkers; i++ {
		go func() {
			defer ep.wg.Done()
			ep.worker()
		}()
	}

	return ep
}

func (ep *elasticPool) Submit(task func()) {
	if task == nil {
		return
	}
	if ep.closed {
		panic("threading: submit on closed pool")
	}
	select {
	case ep.workQueue <- task:
	default:
		ep.wg.Add(1)
		go func() {
			defer ep.wg.Done()
			task()
		}()
	}
}

// Close shuts down warm workers, waits for all in-flight tasks (including
// temporary goroutines) to finish, and returns.
func (ep *elasticPool) Close() {
	ep.closed = true
	ep.closeOnce.Do(func() {
		close(ep.workQueue)
	})
	ep.wg.Wait()
}

func (ep *elasticPool) worker() {
	for task := range ep.workQueue {
		task()
	}
}
