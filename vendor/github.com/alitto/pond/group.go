package pond

import (
	"context"
	"sync"
)

// TaskGroup represents a group of related tasks
type TaskGroup struct {
	pool      *WorkerPool
	waitGroup sync.WaitGroup
}

// Submit adds a task to this group and sends it to the worker pool to be executed
func (g *TaskGroup) Submit(task func()) {
	g.waitGroup.Add(1)

	g.pool.Submit(func() {
		defer g.waitGroup.Done()

		task()
	})
}

// Wait waits until all the tasks in this group have completed
func (g *TaskGroup) Wait() {

	// Wait for all tasks to complete
	g.waitGroup.Wait()
}

// TaskGroupWithContext represents a group of related tasks associated to a context
type TaskGroupWithContext struct {
	TaskGroup
	ctx    context.Context
	cancel context.CancelFunc

	errSync struct {
		once  sync.Once
		guard sync.RWMutex
	}
	err error
}

// Submit adds a task to this group and sends it to the worker pool to be executed
func (g *TaskGroupWithContext) Submit(task func() error) {
	g.waitGroup.Add(1)

	g.pool.Submit(func() {
		defer g.waitGroup.Done()

		// If context has already been cancelled, skip task execution
		select {
		case <-g.ctx.Done():
			return
		default:
		}

		// don't actually ignore errors
		err := task()
		if err != nil {
			g.setError(err)
		}
	})
}

// Wait blocks until either all the tasks submitted to this group have completed,
// one of them returned a non-nil error or the context associated to this group
// was canceled.
func (g *TaskGroupWithContext) Wait() error {

	// Wait for all tasks to complete
	tasksCompleted := make(chan struct{})
	go func() {
		g.waitGroup.Wait()
		tasksCompleted <- struct{}{}
	}()

	select {
	case <-tasksCompleted:
		// If context was provided, cancel it to signal all running tasks to stop
		g.cancel()
	case <-g.ctx.Done():
		g.setError(g.ctx.Err())
	}

	return g.getError()
}

func (g *TaskGroupWithContext) getError() error {
	g.errSync.guard.RLock()
	err := g.err
	g.errSync.guard.RUnlock()
	return err
}

func (g *TaskGroupWithContext) setError(err error) {
	g.errSync.once.Do(func() {
		g.errSync.guard.Lock()
		g.err = err
		g.errSync.guard.Unlock()

		// Cancel execution of any pending task in this group
		g.cancel()
	})
}
