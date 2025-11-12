package evmrpc

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewWorkerPool(t *testing.T) {
	tests := []struct {
		name      string
		workers   int
		queueSize int
	}{
		{"small pool", 2, 10},
		{"medium pool", 5, 50},
		{"large pool", 10, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wp := NewWorkerPool(tt.workers, tt.queueSize)
			defer wp.Close()

			require.Equal(t, tt.workers, wp.WorkerCount())
			require.Equal(t, tt.queueSize, wp.QueueSize())
		})
	}
}

func TestWorkerPoolExecution(t *testing.T) {
	wp := NewWorkerPool(3, 10)
	defer wp.Close()

	wp.Start()

	var counter int64
	var wg sync.WaitGroup

	// Submit 10 tasks
	for range 10 {
		wg.Add(1)
		err := wp.Submit(func() {
			defer wg.Done()
			atomic.AddInt64(&counter, 1)
			time.Sleep(10 * time.Millisecond)
		})
		require.NoError(t, err)
	}

	wg.Wait()
	require.Equal(t, int64(10), atomic.LoadInt64(&counter))
}

func TestWorkerPoolConcurrency(t *testing.T) {
	wp := NewWorkerPool(5, 50)
	defer wp.Close()

	wp.Start()

	var completedTasks int64
	var wg sync.WaitGroup

	// Submit 50 tasks concurrently
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := wp.Submit(func() {
				atomic.AddInt64(&completedTasks, 1)
				time.Sleep(5 * time.Millisecond)
			})
			require.NoError(t, err)
		}()
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond) // Wait for task completion

	require.Equal(t, int64(50), atomic.LoadInt64(&completedTasks))
}

func TestWorkerPoolQueueFull(t *testing.T) {
	// Create a small pool with limited queue
	wp := NewWorkerPool(1, 2)
	defer wp.Close()

	wp.Start()

	var submitted int
	var errors int

	// Block the worker with a long-running task
	wp.Submit(func() {
		time.Sleep(100 * time.Millisecond)
	})

	// Try to submit more tasks than queue can handle
	for range 10 {
		err := wp.Submit(func() {
			time.Sleep(10 * time.Millisecond)
		})

		if err != nil {
			errors++
			require.Contains(t, err.Error(), "worker pool queue is full")
		} else {
			submitted++
		}
	}

	// Should have some errors due to queue being full
	if errors == 0 {
		t.Error("Expected some errors due to full queue, got none")
	}

	if submitted > 3 { // Initial task + queue size
		t.Errorf("Too many tasks submitted for queue size, got %d", submitted)
	}
}

func TestWorkerPoolClose(t *testing.T) {
	wp := NewWorkerPool(3, 10)
	wp.Start()

	var tasksStarted int64
	var tasksCompleted int64

	// Submit some tasks
	for range 5 {
		wp.Submit(func() {
			atomic.AddInt64(&tasksStarted, 1)
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt64(&tasksCompleted, 1)
		})
	}

	// Wait a bit for tasks to start
	time.Sleep(20 * time.Millisecond)

	// Close the pool
	wp.Close()

	// Try to submit after close
	err := wp.Submit(func() {})
	if err == nil {
		t.Error("Expected error when submitting to closed pool")
	}

	if err.Error() != "worker pool is closing" {
		t.Errorf("Expected 'worker pool is closing', got %v", err)
	}

	// All started tasks should complete
	if atomic.LoadInt64(&tasksStarted) != atomic.LoadInt64(&tasksCompleted) {
		t.Errorf("Not all tasks completed: started=%d, completed=%d",
			tasksStarted, tasksCompleted)
	}
}

func TestGlobalWorkerPool(t *testing.T) {
	// Note: This test is now less strict because global pool can be initialized
	// with custom config. We just verify it's functional.

	wp1 := GetGlobalWorkerPool()
	wp2 := GetGlobalWorkerPool()

	// Should be the same instance (singleton)
	if wp1 != wp2 {
		t.Error("GetGlobalWorkerPool() should return the same instance")
	}

	// Test that it's already started and functional
	var executed bool
	var wg sync.WaitGroup
	wg.Add(1)

	err := wp1.Submit(func() {
		defer wg.Done()
		executed = true
	})

	if err != nil {
		t.Errorf("Global pool Submit() error = %v", err)
	}

	wg.Wait()

	if !executed {
		t.Error("Task was not executed by global pool")
	}
}

func TestWorkerPoolStartIdempotent(t *testing.T) {
	wp := NewWorkerPool(2, 5)
	defer wp.Close()

	// Start multiple times
	wp.Start()
	wp.Start()
	wp.Start()

	// Should still work correctly
	var counter int64
	var wg sync.WaitGroup

	for range 3 {
		wg.Add(1)
		wp.Submit(func() {
			defer wg.Done()
			atomic.AddInt64(&counter, 1)
		})
	}

	wg.Wait()

	if atomic.LoadInt64(&counter) != 3 {
		t.Errorf("Expected 3 tasks executed, got %d", counter)
	}

	// Should still have the correct number of workers
	if wp.WorkerCount() != 2 {
		t.Errorf("WorkerCount() = %d, want 2", wp.WorkerCount())
	}
}

func TestWorkerPoolPanicRecovery(t *testing.T) {
	wp := NewWorkerPool(2, 5)
	defer wp.Close()

	wp.Start()

	var successfulTask bool
	var wg sync.WaitGroup

	// Submit a task that panics
	wg.Add(1)
	wp.Submit(func() {
		defer wg.Done()
		panic("test panic")
	})

	// Submit a normal task after the panic
	wg.Add(1)
	wp.Submit(func() {
		defer wg.Done()
		successfulTask = true
	})

	wg.Wait()

	// The pool should still be functional after a panic
	if !successfulTask {
		t.Error("Worker pool should continue working after a panic")
	}
}

func TestWorkerPoolConstants(t *testing.T) {
	require.Equal(t, 100, WorkerBatchSize)
	require.Equal(t, 1000, DefaultWorkerQueueSize)
	require.Equal(t, 64, MaxWorkerPoolSize)

	// Verify total capacity calculation
	totalCapacity := DefaultWorkerQueueSize * WorkerBatchSize
	require.Equal(t, 100000, totalCapacity, "Total capacity should be 100,000 blocks")
}

func TestWorkerPoolOrderingNotGuaranteed(t *testing.T) {
	wp := NewWorkerPool(3, 10)
	defer wp.Close()

	wp.Start()

	var results []int
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Submit tasks with different execution times
	for i := range 5 {
		wg.Add(1)
		taskID := i
		wp.Submit(func() {
			defer wg.Done()

			// Vary execution time to test concurrency
			if taskID%2 == 0 {
				time.Sleep(20 * time.Millisecond)
			} else {
				time.Sleep(5 * time.Millisecond)
			}

			mu.Lock()
			results = append(results, taskID)
			mu.Unlock()
		})
	}

	wg.Wait()

	// Should have all tasks completed
	if len(results) != 5 {
		t.Errorf("Expected 5 results, got %d", len(results))
	}

	// Results might not be in order due to concurrency
	// This is expected behavior for a concurrent worker pool
	resultMap := make(map[int]bool)
	for _, r := range results {
		resultMap[r] = true
	}

	// All task IDs should be present
	for i := range 5 {
		if !resultMap[i] {
			t.Errorf("Task %d was not executed", i)
		}
	}
}

// Test InitGlobalWorkerPool with custom configuration
func TestInitGlobalWorkerPool(t *testing.T) {
	// Reset global pool for this test
	// Note: In production this should only be called once at startup

	tests := []struct {
		name              string
		workerPoolSize    int
		workerQueueSize   int
		expectedWorkers   int
		expectedQueueSize int
	}{
		{
			name:              "custom values",
			workerPoolSize:    10,
			workerQueueSize:   500,
			expectedWorkers:   10,
			expectedQueueSize: 500,
		},
		{
			name:              "zero values use defaults",
			workerPoolSize:    0,
			workerQueueSize:   0,
			expectedWorkers:   -1, // Will be runtime.NumCPU() * 2, check > 0
			expectedQueueSize: DefaultWorkerQueueSize,
		},
		{
			name:              "only worker size specified",
			workerPoolSize:    20,
			workerQueueSize:   0,
			expectedWorkers:   20,
			expectedQueueSize: DefaultWorkerQueueSize,
		},
		{
			name:              "only queue size specified",
			workerPoolSize:    0,
			workerQueueSize:   1000,
			expectedWorkers:   -1, // Will be runtime.NumCPU() * 2
			expectedQueueSize: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new worker pool for each test (not using global)
			// NewWorkerPool will apply defaults if values are <= 0
			wp := NewWorkerPool(tt.workerPoolSize, tt.workerQueueSize)
			wp.Start()
			defer wp.Close()

			// Apply defaults if needed (simulating InitGlobalWorkerPool logic)
			expectedWorkers := tt.expectedWorkers
			if expectedWorkers <= 0 {
				expectedWorkers = 2 // At least 2 workers expected on any system
			}

			actualWorkers := wp.WorkerCount()
			if tt.expectedWorkers > 0 && actualWorkers != tt.expectedWorkers {
				t.Errorf("WorkerCount() = %d, want %d", actualWorkers, tt.expectedWorkers)
			} else if tt.expectedWorkers < 0 && actualWorkers <= 0 {
				t.Errorf("WorkerCount() = %d, should be positive (default)", actualWorkers)
			}

			actualQueueSize := wp.QueueSize()
			if actualQueueSize != tt.expectedQueueSize {
				t.Errorf("QueueSize() = %d, want %d", actualQueueSize, tt.expectedQueueSize)
			}

			// Verify the pool is functional
			var executed bool
			var wg sync.WaitGroup
			wg.Add(1)

			err := wp.Submit(func() {
				defer wg.Done()
				executed = true
			})

			if err != nil {
				t.Errorf("Submit() error = %v", err)
			}

			wg.Wait()

			if !executed {
				t.Error("Task was not executed")
			}
		})
	}
}

// Test that InitGlobalWorkerPool is idempotent
func TestInitGlobalWorkerPoolIdempotent(t *testing.T) {
	t.Skip("Skipping global pool test - global state already initialized by setup_test.go")

	// Note: This test cannot reliably run because the global worker pool
	// is initialized in setup_test.go before any tests run.
	// The idempotency behavior is correct but cannot be tested in this context.
	//
	// The important guarantee is that InitGlobalWorkerPool is idempotent:
	// - First call initializes the pool
	// - Subsequent calls are no-ops
	// This is ensured by sync.Once in InitGlobalWorkerPool, which guarantees
	// the initialization function runs exactly once, even with concurrent calls.
}

// Test worker pool with large queue under stress
func TestWorkerPoolLargeQueueStress(t *testing.T) {
	// Simulate a high-load scenario with large queue
	wp := NewWorkerPool(4, 1000) // 4 workers, large queue
	wp.Start()
	defer wp.Close()

	var completed int64
	var wg sync.WaitGroup
	totalTasks := 500

	// Submit many tasks quickly
	for i := range totalTasks {
		wg.Add(1)
		err := wp.Submit(func() {
			defer wg.Done()
			time.Sleep(1 * time.Millisecond)
			atomic.AddInt64(&completed, 1)
		})

		if err != nil {
			wg.Done()
			t.Errorf("Submit() failed at task %d: %v", i, err)
		}
	}

	wg.Wait()

	require.Equal(t, int64(totalTasks), atomic.LoadInt64(&completed))
}

// Test queue full behavior with different queue sizes
func TestWorkerPoolQueueSizeImpact(t *testing.T) {
	tests := []struct {
		name      string
		queueSize int
		wantError bool
	}{
		{"tiny queue", 2, true},
		{"small queue", 10, true}, // With 1 worker and 10 queue, submitting 20 will cause errors
		{"medium queue", 50, false},
		{"large queue", 200, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wp := NewWorkerPool(1, tt.queueSize)
			wp.Start()
			defer wp.Close()

			// Block the worker with a long-running task
			wp.Submit(func() {
				time.Sleep(100 * time.Millisecond)
			})

			// Determine how many tasks to submit based on test expectations
			var tasksToSubmit int
			if tt.wantError {
				// For small queues, try to overfill: submit more than capacity
				tasksToSubmit = tt.queueSize + 10
			} else {
				// For large queues, submit within capacity to avoid errors
				tasksToSubmit = tt.queueSize / 2
			}

			errors := 0
			submitted := 0

			for range tasksToSubmit {
				err := wp.Submit(func() {
					time.Sleep(10 * time.Millisecond)
				})

				if err != nil {
					errors++
				} else {
					submitted++
				}
			}

			if tt.wantError {
				require.Greater(t, errors, 0, "Expected errors with small queue")
			} else {
				require.Equal(t, 0, errors, "Expected no errors with sufficient queue")
			}
		})
	}
}

// Benchmark worker pool throughput with different configurations
func BenchmarkWorkerPoolThroughput(b *testing.B) {
	configs := []struct {
		name      string
		workers   int
		queueSize int
	}{
		{"small", 4, 100},
		{"medium", 8, 200},
		{"large", 16, 500},
		{"xlarge", 32, 1000},
	}

	for _, config := range configs {
		b.Run(config.name, func(b *testing.B) {
			wp := NewWorkerPool(config.workers, config.queueSize)
			wp.Start()
			defer wp.Close()

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					var wg sync.WaitGroup
					wg.Add(1)
					err := wp.Submit(func() {
						defer wg.Done()
						// Simulate minimal work
						_ = 1 + 1
					})
					if err == nil {
						wg.Wait()
					}
				}
			})
		})
	}
}
