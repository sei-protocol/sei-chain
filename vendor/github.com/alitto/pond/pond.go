package pond

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultIdleTimeout defines the default idle timeout to use when not explicitly specified
	// via the IdleTimeout() option
	defaultIdleTimeout = 5 * time.Second
)

var (
	// ErrSubmitOnStoppedPool is thrown when attempting to submit a task to a pool that has been stopped
	ErrSubmitOnStoppedPool = errors.New("worker pool has been stopped and is no longer accepting tasks")
)

// defaultPanicHandler is the default panic handler
func defaultPanicHandler(panic interface{}) {
	fmt.Printf("Worker exits from a panic: %v\nStack trace: %s\n", panic, string(debug.Stack()))
}

// ResizingStrategy represents a pool resizing strategy
type ResizingStrategy interface {
	Resize(runningWorkers, minWorkers, maxWorkers int) bool
}

// Option represents an option that can be passed when instantiating a worker pool to customize it
type Option func(*WorkerPool)

// IdleTimeout allows to change the idle timeout for a worker pool
func IdleTimeout(idleTimeout time.Duration) Option {
	return func(pool *WorkerPool) {
		pool.idleTimeout = idleTimeout
	}
}

// MinWorkers allows to change the minimum number of workers of a worker pool
func MinWorkers(minWorkers int) Option {
	return func(pool *WorkerPool) {
		pool.minWorkers = minWorkers
	}
}

// Strategy allows to change the strategy used to resize the pool
func Strategy(strategy ResizingStrategy) Option {
	return func(pool *WorkerPool) {
		pool.strategy = strategy
	}
}

// PanicHandler allows to change the panic handler function of a worker pool
func PanicHandler(panicHandler func(interface{})) Option {
	return func(pool *WorkerPool) {
		pool.panicHandler = panicHandler
	}
}

// Context configures a parent context on a worker pool to stop all workers when it is cancelled
func Context(parentCtx context.Context) Option {
	return func(pool *WorkerPool) {
		pool.context, pool.contextCancel = context.WithCancel(parentCtx)
	}
}

// WorkerPool models a pool of workers
type WorkerPool struct {
	// Atomic counters, should be placed first so alignment is guaranteed
	// for atomic operations.
	workerCount         int32
	idleWorkerCount     int32
	waitingTaskCount    uint64
	submittedTaskCount  uint64
	successfulTaskCount uint64
	failedTaskCount     uint64
	// Configurable settings
	maxWorkers    int
	maxCapacity   int
	minWorkers    int
	idleTimeout   time.Duration
	strategy      ResizingStrategy
	panicHandler  func(interface{})
	context       context.Context
	contextCancel context.CancelFunc
	// Private properties
	tasks            chan func()
	tasksCloseOnce   sync.Once
	workersWaitGroup sync.WaitGroup
	tasksWaitGroup   sync.WaitGroup
	mutex            sync.Mutex
	stopped          int32
}

// New creates a worker pool with that can scale up to the given maximum number of workers (maxWorkers).
// The maxCapacity parameter determines the number of tasks that can be submitted to this pool without blocking,
// because it defines the size of the buffered channel used to receive tasks.
// The options parameter can take a list of functions to customize configuration values on this worker pool.
func New(maxWorkers, maxCapacity int, options ...Option) *WorkerPool {

	// Instantiate the pool
	pool := &WorkerPool{
		maxWorkers:   maxWorkers,
		maxCapacity:  maxCapacity,
		idleTimeout:  defaultIdleTimeout,
		strategy:     Eager(),
		panicHandler: defaultPanicHandler,
	}

	// Apply all options
	for _, opt := range options {
		opt(pool)
	}

	// Make sure options are consistent
	if pool.maxWorkers <= 0 {
		pool.maxWorkers = 1
	}
	if pool.minWorkers > pool.maxWorkers {
		pool.minWorkers = pool.maxWorkers
	}
	if pool.maxCapacity < 0 {
		pool.maxCapacity = 0
	}
	if pool.idleTimeout < 0 {
		pool.idleTimeout = defaultIdleTimeout
	}

	// Initialize base context (if not already set)
	if pool.context == nil {
		Context(context.Background())(pool)
	}

	// Create tasks channel
	pool.tasks = make(chan func(), pool.maxCapacity)

	// Start purger goroutine
	pool.workersWaitGroup.Add(1)
	go pool.purge()

	// Start minWorkers workers
	if pool.minWorkers > 0 {
		for i := 0; i < pool.minWorkers; i++ {
			pool.maybeStartWorker(nil)
		}
	}

	return pool
}

// RunningWorkers returns the current number of running workers
func (p *WorkerPool) RunningWorkers() int {
	return int(atomic.LoadInt32(&p.workerCount))
}

// IdleWorkers returns the current number of idle workers
func (p *WorkerPool) IdleWorkers() int {
	return int(atomic.LoadInt32(&p.idleWorkerCount))
}

// MinWorkers returns the minimum number of worker goroutines
func (p *WorkerPool) MinWorkers() int {
	return p.minWorkers
}

// MaxWorkers returns the maximum number of worker goroutines
func (p *WorkerPool) MaxWorkers() int {
	return p.maxWorkers
}

// MaxCapacity returns the maximum number of tasks that can be waiting in the queue
// at any given time (queue size)
func (p *WorkerPool) MaxCapacity() int {
	return p.maxCapacity
}

// Strategy returns the configured pool resizing strategy
func (p *WorkerPool) Strategy() ResizingStrategy {
	return p.strategy
}

// SubmittedTasks returns the total number of tasks submitted since the pool was created
func (p *WorkerPool) SubmittedTasks() uint64 {
	return atomic.LoadUint64(&p.submittedTaskCount)
}

// WaitingTasks returns the current number of tasks in the queue that are waiting to be executed
func (p *WorkerPool) WaitingTasks() uint64 {
	return atomic.LoadUint64(&p.waitingTaskCount)
}

// SuccessfulTasks returns the total number of tasks that have successfully completed their exection
// since the pool was created
func (p *WorkerPool) SuccessfulTasks() uint64 {
	return atomic.LoadUint64(&p.successfulTaskCount)
}

// FailedTasks returns the total number of tasks that completed with panic since the pool was created
func (p *WorkerPool) FailedTasks() uint64 {
	return atomic.LoadUint64(&p.failedTaskCount)
}

// CompletedTasks returns the total number of tasks that have completed their exection either successfully
// or with panic since the pool was created
func (p *WorkerPool) CompletedTasks() uint64 {
	return p.SuccessfulTasks() + p.FailedTasks()
}

// Stopped returns true if the pool has been stopped and is no longer accepting tasks, and false otherwise.
func (p *WorkerPool) Stopped() bool {
	return atomic.LoadInt32(&p.stopped) == 1
}

// Submit sends a task to this worker pool for execution. If the queue is full,
// it will wait until the task is dispatched to a worker goroutine.
func (p *WorkerPool) Submit(task func()) {
	p.submit(task, true)
}

// TrySubmit attempts to send a task to this worker pool for execution. If the queue is full,
// it will not wait for a worker to become idle. It returns true if it was able to dispatch
// the task and false otherwise.
func (p *WorkerPool) TrySubmit(task func()) bool {
	return p.submit(task, false)
}

func (p *WorkerPool) submit(task func(), mustSubmit bool) (submitted bool) {
	if task == nil {
		return
	}

	if p.Stopped() {
		// Pool is stopped and caller must submit the task
		if mustSubmit {
			panic(ErrSubmitOnStoppedPool)
		}
		return
	}

	// Increment submitted and waiting task counters as soon as we receive a task
	atomic.AddUint64(&p.submittedTaskCount, 1)
	atomic.AddUint64(&p.waitingTaskCount, 1)
	p.tasksWaitGroup.Add(1)

	defer func() {
		if !submitted {
			// Task was not sumitted to the pool, decrement submitted and waiting task counters
			atomic.AddUint64(&p.submittedTaskCount, ^uint64(0))
			atomic.AddUint64(&p.waitingTaskCount, ^uint64(0))
			p.tasksWaitGroup.Done()
		}
	}()

	// Start a worker as long as we haven't reached the limit
	if submitted = p.maybeStartWorker(task); submitted {
		return
	}

	if !mustSubmit {
		// Attempt to dispatch to an idle worker without blocking
		select {
		case p.tasks <- task:
			submitted = true
			return
		default:
			// Channel is full and can't wait for an idle worker, so need to exit
			return
		}
	}

	// Submit the task to the tasks channel and wait for it to be picked up by a worker
	p.tasks <- task
	submitted = true
	return
}

// SubmitAndWait sends a task to this worker pool for execution and waits for it to complete
// before returning
func (p *WorkerPool) SubmitAndWait(task func()) {
	if task == nil {
		return
	}

	done := make(chan struct{})
	p.Submit(func() {
		defer close(done)
		task()
	})
	<-done
}

// SubmitBefore attempts to send a task for execution to this worker pool but aborts it
// if the task did not start before the given deadline.
func (p *WorkerPool) SubmitBefore(task func(), deadline time.Duration) {
	if task == nil {
		return
	}

	timer := time.NewTimer(deadline)
	p.Submit(func() {
		select {
		case <-timer.C:
			// Deadline was reached, abort the task
		default:
			// Deadline not reached, execute the task
			defer timer.Stop()

			task()
		}
	})
}

// Stop causes this pool to stop accepting new tasks and signals all workers to exit.
// Tasks being executed by workers will continue until completion (unless the process is terminated).
// Tasks in the queue will not be executed.
func (p *WorkerPool) Stop() {
	go p.stop(false)
}

// StopAndWait causes this pool to stop accepting new tasks and then waits for all tasks in the queue
// to complete before returning.
func (p *WorkerPool) StopAndWait() {
	p.stop(true)
}

// StopAndWaitFor stops this pool and waits until either all tasks in the queue are completed
// or the given deadline is reached, whichever comes first.
func (p *WorkerPool) StopAndWaitFor(deadline time.Duration) {

	// Launch goroutine to detect when worker pool has stopped gracefully
	workersDone := make(chan struct{})
	go func() {
		p.stop(true)
		workersDone <- struct{}{}
	}()

	// Wait until either all workers have exited or the deadline is reached
	select {
	case <-workersDone:
		return
	case <-time.After(deadline):
		p.contextCancel()
		return
	}
}

func (p *WorkerPool) stop(waitForQueuedTasksToComplete bool) {
	// Mark pool as stopped
	atomic.StoreInt32(&p.stopped, 1)

	if waitForQueuedTasksToComplete {
		// Wait for all queued tasks to complete
		p.tasksWaitGroup.Wait()
	}

	// Reset worker count
	p.resetWorkerCount()

	// Terminate all workers & purger goroutine
	p.contextCancel()

	// Wait for all workers & purger goroutine to exit
	p.workersWaitGroup.Wait()

	// close tasks channel (only once, in case multiple concurrent calls to StopAndWait are made)
	p.tasksCloseOnce.Do(func() {
		close(p.tasks)
	})
}

// purge represents the work done by the purger goroutine
func (p *WorkerPool) purge() {
	defer p.workersWaitGroup.Done()

	idleTicker := time.NewTicker(p.idleTimeout)
	defer idleTicker.Stop()

	for {
		select {
		// Timed out waiting for any activity to happen, attempt to stop an idle worker
		case <-idleTicker.C:
			p.maybeStopIdleWorker()
		// Pool context was cancelled, exit
		case <-p.context.Done():
			return
		}
	}
}

// maybeStopIdleWorker attempts to stop an idle worker by sending it a nil task
func (p *WorkerPool) maybeStopIdleWorker() bool {

	if decremented := p.decrementWorkerCount(); !decremented {
		return false
	}

	// Send a nil task to stop an idle worker
	p.tasks <- nil

	return true
}

// maybeStartWorker attempts to create a new worker goroutine to run the given task.
// If the worker pool has reached the maximum number of workers or there are idle workers,
// it will not create a new one.
func (p *WorkerPool) maybeStartWorker(firstTask func()) bool {

	if incremented := p.incrementWorkerCount(); !incremented {
		return false
	}

	if firstTask == nil {
		// Worker starts idle
		atomic.AddInt32(&p.idleWorkerCount, 1)
	}

	// Launch worker goroutine
	go worker(p.context, &p.workersWaitGroup, firstTask, p.tasks, p.executeTask)

	return true
}

// executeTask executes the given task and updates task-related counters
func (p *WorkerPool) executeTask(task func(), isFirstTask bool) {

	defer func() {
		if panic := recover(); panic != nil {
			// Increment failed task count
			atomic.AddUint64(&p.failedTaskCount, 1)

			// Invoke panic handler
			p.panicHandler(panic)

			// Increment idle count
			atomic.AddInt32(&p.idleWorkerCount, 1)
		}
		p.tasksWaitGroup.Done()
	}()

	// Decrement idle count
	if !isFirstTask {
		atomic.AddInt32(&p.idleWorkerCount, -1)
	}

	// Decrement waiting task count
	atomic.AddUint64(&p.waitingTaskCount, ^uint64(0))

	// Execute task
	task()

	// Increment successful task count
	atomic.AddUint64(&p.successfulTaskCount, 1)

	// Increment idle count
	atomic.AddInt32(&p.idleWorkerCount, 1)
}

func (p *WorkerPool) incrementWorkerCount() bool {

	p.mutex.Lock()
	defer p.mutex.Unlock()

	runningWorkerCount := p.RunningWorkers()

	// Reached max workers, do not create a new one
	if runningWorkerCount >= p.maxWorkers {
		return false
	}

	// Idle workers available, do not create a new one
	if runningWorkerCount >= p.minWorkers && runningWorkerCount > 0 && p.IdleWorkers() > 0 {
		return false
	}

	// Execute the resizing strategy to determine if we should create more workers
	if resize := p.strategy.Resize(runningWorkerCount, p.minWorkers, p.maxWorkers); !resize {
		return false
	}

	// Increment worker count
	atomic.AddInt32(&p.workerCount, 1)

	// Increment wait group
	p.workersWaitGroup.Add(1)

	return true
}

func (p *WorkerPool) decrementWorkerCount() bool {

	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.IdleWorkers() <= 0 || p.RunningWorkers() <= p.minWorkers || p.Stopped() {
		return false
	}

	// Decrement worker count
	atomic.AddInt32(&p.workerCount, -1)

	// Decrement idle count
	atomic.AddInt32(&p.idleWorkerCount, -1)

	return true
}

func (p *WorkerPool) resetWorkerCount() {

	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Reset worker count
	atomic.StoreInt32(&p.workerCount, 0)

	// Reset idle count
	atomic.StoreInt32(&p.idleWorkerCount, 0)
}

// Group creates a new task group
func (p *WorkerPool) Group() *TaskGroup {
	return &TaskGroup{
		pool: p,
	}
}

// GroupContext creates a new task group and an associated Context derived from ctx.
//
// The derived Context is canceled the first time a function submitted to the group
// returns a non-nil error or the first time Wait returns, whichever occurs first.
func (p *WorkerPool) GroupContext(ctx context.Context) (*TaskGroupWithContext, context.Context) {

	if ctx == nil {
		panic("a non-nil context needs to be specified when using GroupContext")
	}

	ctx, cancel := context.WithCancel(ctx)
	return &TaskGroupWithContext{
		TaskGroup: TaskGroup{
			pool: p,
		},
		ctx:    ctx,
		cancel: cancel,
	}, ctx
}
