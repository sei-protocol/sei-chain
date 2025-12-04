package evmrpc

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

const (
	// WorkerBatchSize is the number of blocks processed in each batch.
	WorkerBatchSize = 100

	// DefaultWorkerQueueSize is the default size of the task queue.
	DefaultWorkerQueueSize = 1000

	// MaxWorkerPoolSize caps the number of workers to prevent excessive
	// goroutine creation on high-core machines.
	MaxWorkerPoolSize = 64

	// Legacy constant for backward compatibility
	WorkerQueueSize = 200
)

var (
	MaxNumOfWorkers = runtime.NumCPU() * 2 // each worker will handle a batch of WorkerBatchSize blocks
)

// WorkerPool manages a pool of goroutines for concurrent task execution
type WorkerPool struct {
	workers   int
	taskQueue chan func()
	once      sync.Once
	done      chan struct{}
	wg        sync.WaitGroup
	closed    bool
	mu        sync.RWMutex
}

var (
	globalWorkerPool *WorkerPool
	poolOnce         sync.Once
)

// InitGlobalWorkerPool initializes the global worker pool with the given configuration.
// This should be called once during server initialization.
// If workerPoolSize or workerQueueSize is <= 0, defaults are applied.
func InitGlobalWorkerPool(workerPoolSize, workerQueueSize int) {
	poolOnce.Do(func() {
		workers := workerPoolSize
		if workers <= 0 {
			workers = min(MaxWorkerPoolSize, runtime.NumCPU()*2)
		}
		queueSize := workerQueueSize
		if queueSize <= 0 {
			queueSize = DefaultWorkerQueueSize
		}
		globalWorkerPool = &WorkerPool{
			workers:   workers,
			taskQueue: make(chan func(), queueSize),
			done:      make(chan struct{}),
		}
		globalWorkerPool.start()
	})
}

// GetGlobalWorkerPool returns the singleton worker pool instance.
// If not initialized, it creates one with default values.
func GetGlobalWorkerPool() *WorkerPool {
	// Ensure initialization with defaults if not called explicitly
	InitGlobalWorkerPool(0, 0)
	return globalWorkerPool
}

// NewWorkerPool creates a new worker pool with custom configuration
// for testing purposes
func NewWorkerPool(workers, queueSize int) *WorkerPool {
	return &WorkerPool{
		workers:   workers,
		taskQueue: make(chan func(), queueSize),
		done:      make(chan struct{}),
	}
}

// Start initializes and starts the worker goroutines
func (wp *WorkerPool) Start() {
	wp.start()
}

func (wp *WorkerPool) start() {
	wp.once.Do(func() {
		for i := 0; i < wp.workers; i++ {
			wp.wg.Add(1)
			go func() {
				defer wp.wg.Done()
				defer func() {
					if r := recover(); r != nil {
						// Log the panic but don't crash the worker
						fmt.Printf("Worker recovered from panic: %v\n", r)
					}
				}()
				// The worker will exit gracefully when the taskQueue is closed and drained.
				for wrappedTask := range wp.taskQueue {
					func() {
						defer func() {
							if r := recover(); r != nil {
								// Log the panic but continue processing other tasks
								fmt.Printf("Task recovered from panic: %v\n", r)
								GetGlobalMetrics().RecordTaskPanicked()
							}
						}()
						wrappedTask()
					}()
				}
			}()
		}
	})
}

// taskWrapper wraps a task with metrics tracking
type taskWrapper struct {
	task     func()
	queuedAt time.Time
}

// SubmitWithMetrics submits a task with full metrics tracking
func (wp *WorkerPool) SubmitWithMetrics(task func()) error {
	metrics := GetGlobalMetrics()

	// Check if pool is closed first
	wp.mu.RLock()
	if wp.closed {
		wp.mu.RUnlock()
		return fmt.Errorf("worker pool is closing")
	}
	wp.mu.RUnlock()

	queuedAt := time.Now()

	// Wrap the task with metrics
	wrappedTask := func() {
		startedAt := time.Now()
		metrics.RecordTaskStarted(queuedAt)
		defer metrics.RecordTaskCompleted(startedAt)
		task()
	}

	select {
	case wp.taskQueue <- wrappedTask:
		metrics.RecordTaskSubmitted()
		return nil
	case <-wp.done:
		return fmt.Errorf("worker pool is closing")
	default:
		// Queue is full - fail fast
		metrics.RecordTaskRejected()
		return fmt.Errorf("worker pool queue is full")
	}
}

// Submit submits a task to the worker pool with fail-fast behavior
// Returns error if queue is full or pool is closing
func (wp *WorkerPool) Submit(task func()) error {
	// Check if pool is closed first
	wp.mu.RLock()
	if wp.closed {
		wp.mu.RUnlock()
		return fmt.Errorf("worker pool is closing")
	}
	wp.mu.RUnlock()

	select {
	case wp.taskQueue <- task:
		return nil
	case <-wp.done:
		return fmt.Errorf("worker pool is closing")
	default:
		// Queue is full - fail fast
		return fmt.Errorf("worker pool queue is full")
	}
}

// Close gracefully shuts down the worker pool
func (wp *WorkerPool) Close() {
	wp.mu.Lock()
	if wp.closed {
		wp.mu.Unlock()
		return // Already closed
	}
	wp.closed = true
	wp.mu.Unlock()

	close(wp.done)      // Signal that no new tasks should be submitted.
	close(wp.taskQueue) // Close the queue to signal workers to drain and exit.
	wp.wg.Wait()        // Wait for all workers to finish their remaining tasks.
}

// WorkerCount returns the number of workers in the pool
func (wp *WorkerPool) WorkerCount() int {
	return wp.workers
}

// QueueSize returns the capacity of the task queue
func (wp *WorkerPool) QueueSize() int {
	return cap(wp.taskQueue)
}

// QueueDepth returns the current number of tasks in the queue
func (wp *WorkerPool) QueueDepth() int {
	return len(wp.taskQueue)
}

// QueueUtilization returns the percentage of queue capacity in use
func (wp *WorkerPool) QueueUtilization() float64 {
	cap := cap(wp.taskQueue)
	if cap == 0 {
		return 0
	}
	return float64(len(wp.taskQueue)) / float64(cap) * 100
}
