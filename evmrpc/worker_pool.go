package evmrpc

import (
	"fmt"
	"runtime"
	"sync"
)

const (
	WorkerBatchSize        = 100
	DefaultWorkerQueueSize = 200
	DefaultMaxNumOfWorkers = 0 // 0 = runtime.NumCPU() * 2
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
	globalWorkerPool     *WorkerPool
	globalWorkerPoolLock sync.RWMutex
)

// InitGlobalWorkerPool initializes the global worker pool with the given configuration
// This should be called once during server initialization
func InitGlobalWorkerPool(workerPoolSize, workerQueueSize int) {
	globalWorkerPoolLock.Lock()
	defer globalWorkerPoolLock.Unlock()

	if globalWorkerPool != nil {
		// Already initialized
		return
	}

	// Apply defaults if needed
	if workerPoolSize <= 0 {
		workerPoolSize = runtime.NumCPU() * 2
	}
	if workerQueueSize <= 0 {
		workerQueueSize = DefaultWorkerQueueSize
	}

	globalWorkerPool = &WorkerPool{
		workers:   workerPoolSize,
		taskQueue: make(chan func(), workerQueueSize),
		done:      make(chan struct{}),
	}
	globalWorkerPool.start()
}

// GetGlobalWorkerPool returns the singleton worker pool instance
// If not initialized, it creates one with default values
func GetGlobalWorkerPool() *WorkerPool {
	globalWorkerPoolLock.RLock()
	if globalWorkerPool != nil {
		globalWorkerPoolLock.RUnlock()
		return globalWorkerPool
	}
	globalWorkerPoolLock.RUnlock()

	// Initialize with defaults if not already initialized
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
				for task := range wp.taskQueue {
					func() {
						defer func() {
							if r := recover(); r != nil {
								// Log the panic but continue processing other tasks
								fmt.Printf("Task recovered from panic: %v\n", r)
							}
						}()
						task()
					}()
				}
			}()
		}
	})
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
