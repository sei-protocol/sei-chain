package threading

import (
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

type testPool struct {
	name string
	pool Pool
}

func createPools() []testPool {
	return []testPool{
		{"FixedPool", NewFixedPool("test-fixed", 4, 16)},
		{"ElasticPool", NewElasticPool("test-elastic", 4)},
		{"AdHocPool", NewAdHocPool()},
	}
}

func TestPool_AllTasksComplete(t *testing.T) {
	for _, tc := range createPools() {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.pool.Close()
			const n = 100
			var counter atomic.Int64
			var wg sync.WaitGroup
			wg.Add(n)

			for i := 0; i < n; i++ {
				tc.pool.Submit(func() {
					counter.Add(1)
					wg.Done()
				})
			}

			waitOrFail(t, &wg)
			if got := counter.Load(); got != n {
				t.Errorf("expected %d tasks completed, got %d", n, got)
			}
		})
	}
}

func TestPool_BlockedTasksDontCompleteUntilUnblocked(t *testing.T) {
	for _, tc := range createPools() {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.pool.Close()
			blocker := make(chan struct{})
			var counter atomic.Int64
			var wg sync.WaitGroup
			wg.Add(2)

			for i := 0; i < 2; i++ {
				tc.pool.Submit(func() {
					defer wg.Done()
					<-blocker
					counter.Add(1)
				})
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
	pool := NewFixedPool("test-fixed-block", workers, queueSize)
	defer pool.Close()

	blocker := make(chan struct{})
	var completed atomic.Int64
	var wg sync.WaitGroup

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		pool.Submit(func() {
			defer wg.Done()
			<-blocker
			completed.Add(1)
		})
	}
	time.Sleep(10 * time.Millisecond)

	wg.Add(queueSize)
	for i := 0; i < queueSize; i++ {
		pool.Submit(func() {
			defer wg.Done()
			<-blocker
			completed.Add(1)
		})
	}

	wg.Add(1)
	submitDone := make(chan struct{})
	start := time.Now()
	go func() {
		pool.Submit(func() {
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
	pool := NewElasticPool("test-elastic-scale", warmWorkers)
	defer pool.Close()

	blocker := make(chan struct{})
	var started atomic.Int64
	var wg sync.WaitGroup
	wg.Add(totalTasks)

	for i := 0; i < totalTasks; i++ {
		pool.Submit(func() {
			defer wg.Done()
			started.Add(1)
			<-blocker
		})
	}

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

func TestPool_NilTaskIsIgnored(t *testing.T) {
	for _, tc := range createPools() {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.pool.Close()
			tc.pool.Submit(nil)
		})
	}
}

func TestElasticPool_CloseWaitsForAdHocGoroutines(t *testing.T) {
	const warmWorkers = 1
	const totalTasks = 20
	pool := NewElasticPool("test-elastic-close", warmWorkers)

	blocker := make(chan struct{})
	var completed atomic.Int64

	// Fill the warm worker, then submit more tasks to force ad-hoc goroutines.
	for i := 0; i < totalTasks; i++ {
		pool.Submit(func() {
			<-blocker
			completed.Add(1)
		})
	}

	time.Sleep(50 * time.Millisecond)

	close(blocker)

	closeDone := make(chan struct{})
	go func() {
		pool.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for Close to return")
	}

	if got := completed.Load(); got != totalTasks {
		t.Errorf("expected %d tasks completed before Close returned, got %d", totalTasks, got)
	}
}

func TestFixedPool_CloseDrainsPendingTasks(t *testing.T) {
	pool := NewFixedPool("test-drain", 1, 100)

	blocker := make(chan struct{})
	var completed atomic.Int64

	const pendingTasks = 50
	var wg sync.WaitGroup
	wg.Add(1 + pendingTasks)

	pool.Submit(func() {
		defer wg.Done()
		<-blocker
		completed.Add(1)
	})
	time.Sleep(10 * time.Millisecond)

	for i := 0; i < pendingTasks; i++ {
		pool.Submit(func() {
			defer wg.Done()
			completed.Add(1)
		})
	}

	// Unblock the worker, then Close should drain all buffered tasks.
	close(blocker)
	pool.Close()

	expected := int64(1 + pendingTasks)
	if got := completed.Load(); got != expected {
		t.Errorf("expected %d tasks drained on Close, got %d", expected, got)
	}
}

func TestFixedPool_CloseBlocksUntilDrained(t *testing.T) {
	pool := NewFixedPool("test-close-blocks", 2, 0)

	var completed atomic.Int64
	blocker := make(chan struct{})

	pool.Submit(func() {
		<-blocker
		completed.Add(1)
	})
	pool.Submit(func() {
		<-blocker
		completed.Add(1)
	})
	time.Sleep(10 * time.Millisecond)

	closeDone := make(chan struct{})
	go func() {
		pool.Close()
		close(closeDone)
	}()

	time.Sleep(20 * time.Millisecond)
	select {
	case <-closeDone:
		t.Fatal("Close returned while tasks are still running")
	default:
	}

	close(blocker)

	select {
	case <-closeDone:
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for Close to return")
	}

	if got := completed.Load(); got != 2 {
		t.Errorf("expected 2 tasks completed, got %d", got)
	}
}
