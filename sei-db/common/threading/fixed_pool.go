package threading

import "sync"

var _ Pool = (*fixedPool)(nil)

// fixedPool is a pool of workers that can be used to execute tasks concurrently.
// More efficient than spawning large numbers of short lived goroutines.
type fixedPool struct {
	workQueue chan func()
	wg        sync.WaitGroup
	closeOnce sync.Once
	closed    bool
}

// Create a new work pool.
func NewFixedPool(
	// The name of the work pool. Used for metrics.
	name string,
	// The number of workers to create.
	workers int,
	// The size of the work queue. Once full, Submit will block until a slot is available.
	queueSize int,
) Pool {

	if workers <= 0 {
		workers = 1
	}

	workQueue := make(chan func(), queueSize)
	fp := &fixedPool{
		workQueue: workQueue,
	}

	fp.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer fp.wg.Done()
			fp.worker()
		}()
	}

	return fp
}

func (fp *fixedPool) Submit(task func()) {
	if task == nil {
		return
	}
	if fp.closed {
		panic("threading: submit on closed pool")
	}
	fp.workQueue <- task
}

func (fp *fixedPool) Close() {
	fp.closed = true
	fp.closeOnce.Do(func() {
		close(fp.workQueue)
	})
	fp.wg.Wait()
}

func (fp *fixedPool) worker() {
	for task := range fp.workQueue {
		task()
	}
}
