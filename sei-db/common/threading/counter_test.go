package threading

import (
	"context"
	"math"
	"testing"
	"time"
)

// awaitResult holds the values returned by one Await call.
type awaitResult struct {
	reached bool
	value   uint64
	err     error
}

// awaitAsync runs Await in a goroutine and returns a channel that receives its result.
func awaitAsync(ctx context.Context, counter ReadOnlyCounter, target uint64) <-chan awaitResult {
	result := make(chan awaitResult, 1)
	go func() {
		reached, value, err := counter.Await(ctx, target)
		result <- awaitResult{reached: reached, value: value, err: err}
	}()
	return result
}

// requireBlocked fails the test if result yields a value within a short grace period.
func requireBlocked(t *testing.T, result <-chan awaitResult) {
	t.Helper()
	select {
	case got := <-result:
		t.Fatalf("Await returned early with %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
}

// receiveResult waits for result to yield and returns it.
func receiveResult(t *testing.T, result <-chan awaitResult) awaitResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for Await to return")
		return awaitResult{}
	}
}

// requireAwait fails the test unless got returned without error and with the expected reached and value.
func requireAwait(t *testing.T, got awaitResult, wantReached bool, wantValue uint64) {
	t.Helper()
	if got.err != nil {
		t.Fatalf("unexpected Await error: %v", got.err)
	}
	if got.reached != wantReached || got.value != wantValue {
		t.Fatalf("expected Await to return (%v, %d), got (%v, %d)",
			wantReached, wantValue, got.reached, got.value)
	}
}

// awaitNow calls Await on the current goroutine and returns its result.
func awaitNow(ctx context.Context, counter ReadOnlyCounter, target uint64) awaitResult {
	reached, value, err := counter.Await(ctx, target)
	return awaitResult{reached: reached, value: value, err: err}
}

func TestCounter_GetReflectsIncrements(t *testing.T) {
	counter := NewCounter(5)
	if got := counter.Get(); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
	for i := uint64(6); i <= 10; i++ {
		newValue, err := counter.Increment()
		if err != nil {
			t.Fatalf("unexpected Increment error: %v", err)
		}
		if newValue != i {
			t.Fatalf("expected Increment to return %d, got %d", i, newValue)
		}
		if got := counter.Get(); got != i {
			t.Fatalf("expected %d, got %d", i, got)
		}
	}
}

func TestCounter_IncrementN(t *testing.T) {
	counter := NewCounter(5)

	newValue, err := counter.IncrementN(3)
	if err != nil {
		t.Fatalf("unexpected IncrementN error: %v", err)
	}
	if newValue != 8 || counter.Get() != 8 {
		t.Fatalf("expected 8, got returned %d and Get %d", newValue, counter.Get())
	}

	newValue, err = counter.IncrementN(0)
	if err != nil {
		t.Fatalf("unexpected IncrementN error: %v", err)
	}
	if newValue != 8 || counter.Get() != 8 {
		t.Fatalf("expected 8, got returned %d and Get %d", newValue, counter.Get())
	}
}

func TestCounter_IncrementNWakesAwaitBelowNewValue(t *testing.T) {
	counter := NewCounter(0)
	result := awaitAsync(context.Background(), counter, 2)
	requireBlocked(t, result)

	if _, err := counter.IncrementN(3); err != nil {
		t.Fatalf("unexpected IncrementN error: %v", err)
	}
	requireAwait(t, receiveResult(t, result), true, 3)
}

func TestCounter_IncrementNErrorsOnOverflow(t *testing.T) {
	counter := NewCounter(math.MaxUint64 - 2)
	if _, err := counter.IncrementN(3); err == nil {
		t.Fatal("expected IncrementN to return an error")
	}
	if got := counter.Get(); got != math.MaxUint64-2 {
		t.Fatalf("expected %d, got %d", uint64(math.MaxUint64-2), got)
	}
	newValue, err := counter.IncrementN(2)
	if err != nil {
		t.Fatalf("unexpected IncrementN error: %v", err)
	}
	if newValue != math.MaxUint64 {
		t.Fatalf("expected %d, got %d", uint64(math.MaxUint64), newValue)
	}
}

func TestCounter_AwaitReturnsImmediatelyWhenReached(t *testing.T) {
	counter := NewCounter(5)
	for _, target := range []uint64{0, 4, 5} {
		requireAwait(t, awaitNow(context.Background(), counter, target), true, 5)
	}
}

func TestCounter_AwaitBlocksUntilTargetReached(t *testing.T) {
	counter := NewCounter(0)
	result := awaitAsync(context.Background(), counter, 3)

	for i := 0; i < 2; i++ {
		if _, err := counter.Increment(); err != nil {
			t.Fatalf("unexpected Increment error: %v", err)
		}
		requireBlocked(t, result)
	}

	if _, err := counter.Increment(); err != nil {
		t.Fatalf("unexpected Increment error: %v", err)
	}
	requireAwait(t, receiveResult(t, result), true, 3)
}

func TestCounter_AwaitErrorsOnCancelWhileBlocked(t *testing.T) {
	counter := NewCounter(0)
	ctx, cancel := context.WithCancel(context.Background())
	result := awaitAsync(ctx, counter, 1)
	requireBlocked(t, result)

	cancel()
	if got := receiveResult(t, result); got.err == nil {
		t.Fatal("expected Await to return an error")
	}
}

func TestCounter_AwaitErrorsOnCancelledContext(t *testing.T) {
	counter := NewCounter(0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := awaitNow(ctx, counter, 1); got.err == nil {
		t.Fatal("expected Await to return an error")
	}
}

func TestCounter_IncrementErrorsAtMaximum(t *testing.T) {
	counter := NewCounter(math.MaxUint64)
	if _, err := counter.Increment(); err == nil {
		t.Fatal("expected Increment to return an error")
	}
	if got := counter.Get(); got != math.MaxUint64 {
		t.Fatalf("expected %d, got %d", uint64(math.MaxUint64), got)
	}
}

func TestCounter_Sealed(t *testing.T) {
	counter := NewCounter(5)
	if err := counter.Seal(); err != nil {
		t.Fatalf("unexpected Seal error: %v", err)
	}

	requireAwait(t, awaitNow(context.Background(), counter, 5), true, 5)
	requireAwait(t, awaitNow(context.Background(), counter, 6), false, 5)
	if _, err := counter.Increment(); err == nil {
		t.Fatal("expected Increment to return an error after Seal")
	}
	if _, err := counter.IncrementN(0); err == nil {
		t.Fatal("expected IncrementN to return an error after Seal")
	}
	if err := counter.Seal(); err == nil {
		t.Fatal("expected second Seal to return an error")
	}
	if got := counter.Get(); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
}

func TestCounter_SealReleasesBlockedAwait(t *testing.T) {
	counter := NewCounter(0)
	result := awaitAsync(context.Background(), counter, 1)
	requireBlocked(t, result)

	if err := counter.Seal(); err != nil {
		t.Fatalf("unexpected Seal error: %v", err)
	}
	requireAwait(t, receiveResult(t, result), false, 0)
}

func TestNoOpCounter(t *testing.T) {
	counter := NoOpCounter{}
	if got := counter.Get(); got != math.MaxUint64 {
		t.Fatalf("expected %d, got %d", uint64(math.MaxUint64), got)
	}
	for _, target := range []uint64{0, 1, math.MaxUint64} {
		requireAwait(t, awaitNow(context.Background(), counter, target), true, math.MaxUint64)
	}
}
