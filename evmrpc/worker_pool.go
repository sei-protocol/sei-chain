package evmrpc

import (
	"fmt"
	"runtime"
	"sync"
)

const (
	// WorkerBatchSize is the number of blocks processed in each batch.
	// Used in filter.go for batch processing of block queries.
	WorkerBatchSize = 100

	// DefaultWorkerQueueSize is the default size of the task queue.
	// This represents the number of tasks (not blocks) that can be queued.
	// Total capacity = DefaultWorkerQueueSize * WorkerBatchSize blocks
	// Example: 1000 tasks * 100 blocks/task = 100,000 blocks can be buffered
	//
	// Memory footprint estimate:
	// - Queue channel overhead: ~8KB (1000 * 8 bytes per channel slot)
	// - Each task closure: ~24 bytes
	// - Total queue memory: ~32KB (negligible)
	// Note: Actual memory usage depends on block data processed by workers
	DefaultWorkerQueueSize = 1000

	// MaxWorkerPoolSize caps the number of workers to prevent excessive
	// goroutine creation on high-core machines. Tasks are primarily I/O bound
	// (fetching and processing block logs), so 2x CPU cores can be excessive.
	MaxWorkerPoolSize = 64
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
	poolOnce         sync.Once
	globalWorkerPool *WorkerPool
)

// InitGlobalWorkerPool initializes the global worker pool with the given configuration.
// This should be called once during server initialization.
// If workerPoolSize or workerQueueSize is <= 0, defaults are applied by NewWorkerPool.
// Using sync.Once ensures initialization happens exactly once, even with concurrent calls.
func InitGlobalWorkerPool(workerPoolSize, workerQueueSize int) {
	poolOnce.Do(func() {
		// NewWorkerPool will apply defaults if needed
		globalWorkerPool = NewWorkerPool(workerPoolSize, workerQueueSize)
		globalWorkerPool.start()
	})
}

// GetGlobalWorkerPool returns the singleton worker pool instance.
// If not initialized, it creates one with default values:
// - Worker count: min(MaxWorkerPoolSize, runtime.NumCPU() * 2)
// - Queue size: DefaultWorkerQueueSize
func GetGlobalWorkerPool() *WorkerPool {
	// Ensure initialization with defaults if not called explicitly
	// sync.Once guarantees this is thread-safe
	InitGlobalWorkerPool(0, 0)
	return globalWorkerPool
}

// NewWorkerPool creates a new worker pool with the specified number of workers and queue size.
// If workers or queueSize is <= 0, defaults are applied:
// - workers: min(MaxWorkerPoolSize, runtime.NumCPU() * 2)
// - queueSize: DefaultWorkerQueueSize
func NewWorkerPool(workers, queueSize int) *WorkerPool {
	// Apply defaults if invalid
	if workers <= 0 {
		workers = min(MaxWorkerPoolSize, runtime.NumCPU()*2)
	}
	if queueSize <= 0 {
		queueSize = DefaultWorkerQueueSize
	}

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
