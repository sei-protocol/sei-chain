package threading

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testTimeout = 10 * time.Second

func waitOrFail(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
	}()
	select {
	case <-ch:
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for tasks to complete")
	}
}

func createPools(ctx context.Context) []struct {
	name string
	pool Pool
} {
	return []struct {
		name string
		pool Pool
	}{
		{"FixedPool", NewFixedPool(ctx, "test-fixed", 4, 16)},
		{"ElasticPool", NewElasticPool(ctx, "test-elastic", 4)},
		{"AdHocPool", NewAdHocPool()},
	}
}

func TestPool_AllTasksComplete(t *testing.T) {
	for _, tc := range createPools(t.Context()) {
		t.Run(tc.name, func(t *testing.T) {
			const n = 100
			var counter atomic.Int64
			var wg sync.WaitGroup
			wg.Add(n)

			for i := 0; i < n; i++ {
				err := tc.pool.Submit(t.Context(), func() {
					counter.Add(1)
					wg.Done()
				})
				if err != nil {
					t.Fatalf("Submit failed: %v", err)
				}
			}

			waitOrFail(t, &wg)
			if got := counter.Load(); got != n {
				t.Errorf("expected %d tasks completed, got %d", n, got)
			}
		})
	}
}

func TestPool_BlockedTasksDontCompleteUntilUnblocked(t *testing.T) {
	for _, tc := range createPools(t.Context()) {
		t.Run(tc.name, func(t *testing.T) {
			blocker := make(chan struct{})
			var counter atomic.Int64
			var wg sync.WaitGroup
			wg.Add(2)

			for i := 0; i < 2; i++ {
				err := tc.pool.Submit(t.Context(), func() {
					defer wg.Done()
					<-blocker
					counter.Add(1)
				})
				if err != nil {
					t.Fatalf("Submit failed: %v", err)
				}
			}

			time.Sleep(10 * time.Millisecond)
			if got := counter.Load(); got != 0 {
				t.Errorf("expected counter=0 while blocked, got %d", got)
			}

			close(blocker)
			waitOrFail(t, &wg)

			if got := counter.Load(); got != 2 {
				t.Errorf("expected counter=2 after unblock, got %d", got)
			}
		})
	}
}

func TestFixedPool_SubmitBlocksWhenFull(t *testing.T) {
	const workers = 2
	const queueSize = 2
	pool := NewFixedPool(t.Context(), "test-fixed-block", workers, queueSize)

	blocker := make(chan struct{})
	var completed atomic.Int64
	var wg sync.WaitGroup

	// Phase 1: occupy all workers with blocking tasks.
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		err := pool.Submit(t.Context(), func() {
			defer wg.Done()
			<-blocker
			completed.Add(1)
		})
		if err != nil {
			t.Fatalf("Submit failed: %v", err)
		}
	}
	time.Sleep(10 * time.Millisecond)

	// Phase 2: fill the queue buffer.
	wg.Add(queueSize)
	for i := 0; i < queueSize; i++ {
		err := pool.Submit(t.Context(), func() {
			defer wg.Done()
			<-blocker
			completed.Add(1)
		})
		if err != nil {
			t.Fatalf("Submit failed: %v", err)
		}
	}

	// Phase 3: the next Submit must block — queue full, all workers busy.
	wg.Add(1)
	submitDone := make(chan struct{})
	start := time.Now()
	go func() {
		_ = pool.Submit(t.Context(), func() {
			defer wg.Done()
			<-blocker
			completed.Add(1)
		})
		close(submitDone)
	}()

	time.Sleep(20 * time.Millisecond)
	select {
	case <-submitDone:
		t.Fatalf("Submit returned after only %v; expected it to block", time.Since(start))
	default:
	}

	close(blocker)
	select {
	case <-submitDone:
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for blocked submit to complete")
	}
	waitOrFail(t, &wg)

	expected := int64(workers + queueSize + 1)
	if got := completed.Load(); got != expected {
		t.Errorf("expected %d tasks completed, got %d", expected, got)
	}
}

func TestElasticPool_ScalesBeyondWarmWorkers(t *testing.T) {
	const warmWorkers = 2
	const totalTasks = 10
	pool := NewElasticPool(t.Context(), "test-elastic-scale", warmWorkers)

	blocker := make(chan struct{})
	var started atomic.Int64
	var wg sync.WaitGroup
	wg.Add(totalTasks)

	for i := 0; i < totalTasks; i++ {
		err := pool.Submit(t.Context(), func() {
			defer wg.Done()
			started.Add(1)
			<-blocker
		})
		if err != nil {
			t.Fatalf("Submit failed: %v", err)
		}
	}

	// All tasks should start promptly — elastic pool spawns extra goroutines.
	time.Sleep(50 * time.Millisecond)
	if got := started.Load(); got <= int64(warmWorkers) {
		t.Errorf("expected started > %d (warm workers), got %d", warmWorkers, got)
	}
	if got := started.Load(); got != totalTasks {
		t.Errorf("expected all %d tasks started, got %d", totalTasks, got)
	}

	close(blocker)
	waitOrFail(t, &wg)
}

func TestFixedPool_SubmitReturnsErrorOnCancelledContext(t *testing.T) {
	poolCtx, poolCancel := context.WithCancel(t.Context())
	defer poolCancel()

	// Use a zero-buffer queue so submit blocks once the worker is busy.
	pool := NewFixedPool(poolCtx, "test-ctx", 1, 0)

	blocker := make(chan struct{})
	defer close(blocker)

	_ = pool.Submit(poolCtx, func() { <-blocker })
	time.Sleep(10 * time.Millisecond)

	submitCtx, submitCancel := context.WithCancel(t.Context())
	submitCancel()

	err := pool.Submit(submitCtx, func() {})
	if err == nil {
		t.Error("expected error from Submit with cancelled context")
	}
}

func TestPool_SubmitAfterShutdown(t *testing.T) {
	for _, tc := range []struct {
		name string
		pool Pool
	}{
		{"FixedPool", func() Pool {
			ctx, cancel := context.WithCancel(t.Context())
			p := NewFixedPool(ctx, "test-shutdown", 2, 4)
			cancel()
			return p
		}()},
		{"ElasticPool", func() Pool {
			ctx, cancel := context.WithCancel(t.Context())
			p := NewElasticPool(ctx, "test-shutdown", 2)
			cancel()
			return p
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			time.Sleep(10 * time.Millisecond)
			// Must not panic. May or may not return an error.
			_ = tc.pool.Submit(t.Context(), func() {})
		})
	}
}
