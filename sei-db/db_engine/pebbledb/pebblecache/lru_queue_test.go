package pebblecache

import (
	"fmt"
	"testing"
)

func TestLRUQueueIsolatesFromCallerMutation(t *testing.T) {
	lru := NewLRUQueue()

	key := []byte("a")
	lru.Push(key, 1)
	key[0] = 'z'

	if got := lru.PopLeastRecentlyUsed(); got != "a" {
		t.Fatalf("pop after mutating caller key = %q, want %q", got, "a")
	}
}

func TestNewLRUQueueStartsEmpty(t *testing.T) {
	lru := NewLRUQueue()

	if got := lru.GetCount(); got != 0 {
		t.Fatalf("GetCount() = %d, want 0", got)
	}
	if got := lru.GetTotalSize(); got != 0 {
		t.Fatalf("GetTotalSize() = %d, want 0", got)
	}
}

func TestPopLeastRecentlyUsedPanicsOnEmptyQueue(t *testing.T) {
	lru := NewLRUQueue()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on pop from empty queue, but none occurred")
		}
	}()

	lru.PopLeastRecentlyUsed()
}

func TestPopLeastRecentlyUsedPanicsAfterDrain(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("x"), 1)
	lru.PopLeastRecentlyUsed()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on pop from drained queue, but none occurred")
		}
	}()

	lru.PopLeastRecentlyUsed()
}

func TestPushSingleElement(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("only"), 42)

	if got := lru.GetCount(); got != 1 {
		t.Fatalf("GetCount() = %d, want 1", got)
	}
	if got := lru.GetTotalSize(); got != 42 {
		t.Fatalf("GetTotalSize() = %d, want 42", got)
	}
	if got := lru.PopLeastRecentlyUsed(); got != "only" {
		t.Fatalf("pop = %q, want %q", got, "only")
	}
}

func TestPushDuplicateDecreasesSize(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("k"), 100)
	lru.Push([]byte("k"), 30)

	if got := lru.GetCount(); got != 1 {
		t.Fatalf("GetCount() = %d, want 1", got)
	}
	if got := lru.GetTotalSize(); got != 30 {
		t.Fatalf("GetTotalSize() = %d, want 30", got)
	}
}

func TestPushDuplicateMovesToBack(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 1)
	lru.Push([]byte("b"), 1)
	lru.Push([]byte("c"), 1)

	// Re-push "a" — should move it behind "b" and "c"
	lru.Push([]byte("a"), 1)

	if got := lru.PopLeastRecentlyUsed(); got != "b" {
		t.Fatalf("pop = %q, want %q", got, "b")
	}
	if got := lru.PopLeastRecentlyUsed(); got != "c" {
		t.Fatalf("pop = %q, want %q", got, "c")
	}
	if got := lru.PopLeastRecentlyUsed(); got != "a" {
		t.Fatalf("pop = %q, want %q", got, "a")
	}
}

func TestPushZeroSize(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("z"), 0)

	if got := lru.GetCount(); got != 1 {
		t.Fatalf("GetCount() = %d, want 1", got)
	}
	if got := lru.GetTotalSize(); got != 0 {
		t.Fatalf("GetTotalSize() = %d, want 0", got)
	}

	if got := lru.PopLeastRecentlyUsed(); got != "z" {
		t.Fatalf("pop = %q, want %q", got, "z")
	}
	if got := lru.GetTotalSize(); got != 0 {
		t.Fatalf("GetTotalSize() after pop = %d, want 0", got)
	}
}

func TestPushEmptyKey(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte(""), 5)

	if got := lru.GetCount(); got != 1 {
		t.Fatalf("GetCount() = %d, want 1", got)
	}
	if got := lru.PopLeastRecentlyUsed(); got != "" {
		t.Fatalf("pop = %q, want %q", got, "")
	}
}

func TestPushRepeatedUpdatesToSameKey(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("k"), 1)
	lru.Push([]byte("k"), 2)
	lru.Push([]byte("k"), 3)
	lru.Push([]byte("k"), 4)

	if got := lru.GetCount(); got != 1 {
		t.Fatalf("GetCount() = %d, want 1", got)
	}
	if got := lru.GetTotalSize(); got != 4 {
		t.Fatalf("GetTotalSize() = %d, want 4 (last push)", got)
	}
}

func TestTouchNonexistentKeyIsNoop(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 1)

	// Should not panic or change state.
	lru.Touch([]byte("missing"))

	if got := lru.GetCount(); got != 1 {
		t.Fatalf("GetCount() = %d, want 1", got)
	}
	if got := lru.PopLeastRecentlyUsed(); got != "a" {
		t.Fatalf("pop = %q, want %q", got, "a")
	}
}

func TestTouchOnEmptyQueueIsNoop(t *testing.T) {
	lru := NewLRUQueue()
	lru.Touch([]byte("ghost"))

	if got := lru.GetCount(); got != 0 {
		t.Fatalf("GetCount() = %d, want 0", got)
	}
}

func TestTouchSingleElement(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("solo"), 10)
	lru.Touch([]byte("solo"))

	if got := lru.GetCount(); got != 1 {
		t.Fatalf("GetCount() = %d, want 1", got)
	}
	if got := lru.PopLeastRecentlyUsed(); got != "solo" {
		t.Fatalf("pop = %q, want %q", got, "solo")
	}
}

func TestTouchDoesNotAffectSizeOrCount(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 3)
	lru.Push([]byte("b"), 7)

	lru.Touch([]byte("a"))

	if got := lru.GetCount(); got != 2 {
		t.Fatalf("GetCount() = %d, want 2", got)
	}
	if got := lru.GetTotalSize(); got != 10 {
		t.Fatalf("GetTotalSize() = %d, want 10", got)
	}
}

func TestMultipleTouchesChangeOrder(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 1)
	lru.Push([]byte("b"), 1)
	lru.Push([]byte("c"), 1)

	// Order: a, b, c
	lru.Touch([]byte("a")) // Order: b, c, a
	lru.Touch([]byte("b")) // Order: c, a, b

	if got := lru.PopLeastRecentlyUsed(); got != "c" {
		t.Fatalf("pop = %q, want %q", got, "c")
	}
	if got := lru.PopLeastRecentlyUsed(); got != "a" {
		t.Fatalf("pop = %q, want %q", got, "a")
	}
	if got := lru.PopLeastRecentlyUsed(); got != "b" {
		t.Fatalf("pop = %q, want %q", got, "b")
	}
}

func TestTouchAlreadyMostRecentIsNoop(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 1)
	lru.Push([]byte("b"), 1)

	lru.Touch([]byte("b")) // "b" is already at back

	if got := lru.PopLeastRecentlyUsed(); got != "a" {
		t.Fatalf("pop = %q, want %q", got, "a")
	}
	if got := lru.PopLeastRecentlyUsed(); got != "b" {
		t.Fatalf("pop = %q, want %q", got, "b")
	}
}

func TestPopDecrementsCountAndSize(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 10)
	lru.Push([]byte("b"), 20)
	lru.Push([]byte("c"), 30)

	lru.PopLeastRecentlyUsed()

	if got := lru.GetCount(); got != 2 {
		t.Fatalf("GetCount() = %d, want 2", got)
	}
	if got := lru.GetTotalSize(); got != 50 {
		t.Fatalf("GetTotalSize() = %d, want 50", got)
	}

	lru.PopLeastRecentlyUsed()

	if got := lru.GetCount(); got != 1 {
		t.Fatalf("GetCount() = %d, want 1", got)
	}
	if got := lru.GetTotalSize(); got != 30 {
		t.Fatalf("GetTotalSize() = %d, want 30", got)
	}
}

func TestPopFIFOOrderWithoutTouches(t *testing.T) {
	lru := NewLRUQueue()
	keys := []string{"first", "second", "third", "fourth"}
	for _, k := range keys {
		lru.Push([]byte(k), 1)
	}

	for _, want := range keys {
		if got := lru.PopLeastRecentlyUsed(); got != want {
			t.Fatalf("pop = %q, want %q", got, want)
		}
	}
}

func TestPushAfterDrain(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 5)
	lru.PopLeastRecentlyUsed()

	// Queue is empty; push new entries.
	lru.Push([]byte("x"), 10)
	lru.Push([]byte("y"), 20)

	if got := lru.GetCount(); got != 2 {
		t.Fatalf("GetCount() = %d, want 2", got)
	}
	if got := lru.GetTotalSize(); got != 30 {
		t.Fatalf("GetTotalSize() = %d, want 30", got)
	}
	if got := lru.PopLeastRecentlyUsed(); got != "x" {
		t.Fatalf("pop = %q, want %q", got, "x")
	}
}

func TestPushPreviouslyPoppedKey(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("recycled"), 5)
	lru.PopLeastRecentlyUsed()

	lru.Push([]byte("recycled"), 99)

	if got := lru.GetCount(); got != 1 {
		t.Fatalf("GetCount() = %d, want 1", got)
	}
	if got := lru.GetTotalSize(); got != 99 {
		t.Fatalf("GetTotalSize() = %d, want 99", got)
	}
	if got := lru.PopLeastRecentlyUsed(); got != "recycled" {
		t.Fatalf("pop = %q, want %q", got, "recycled")
	}
}

func TestInterleavedPushAndPop(t *testing.T) {
	lru := NewLRUQueue()

	lru.Push([]byte("a"), 1)
	lru.Push([]byte("b"), 2)

	if got := lru.PopLeastRecentlyUsed(); got != "a" {
		t.Fatalf("pop = %q, want %q", got, "a")
	}

	lru.Push([]byte("c"), 3)

	if got := lru.GetCount(); got != 2 {
		t.Fatalf("GetCount() = %d, want 2", got)
	}
	if got := lru.GetTotalSize(); got != 5 {
		t.Fatalf("GetTotalSize() = %d, want 5", got)
	}

	// "b" was pushed before "c"
	if got := lru.PopLeastRecentlyUsed(); got != "b" {
		t.Fatalf("pop = %q, want %q", got, "b")
	}
	if got := lru.PopLeastRecentlyUsed(); got != "c" {
		t.Fatalf("pop = %q, want %q", got, "c")
	}
}

func TestTouchThenPushSameKey(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 1)
	lru.Push([]byte("b"), 1)

	lru.Touch([]byte("a"))    // order: b, a
	lru.Push([]byte("a"), 50) // updates size, stays at back

	if got := lru.GetCount(); got != 2 {
		t.Fatalf("GetCount() = %d, want 2", got)
	}
	if got := lru.GetTotalSize(); got != 51 {
		t.Fatalf("GetTotalSize() = %d, want 51", got)
	}
	if got := lru.PopLeastRecentlyUsed(); got != "b" {
		t.Fatalf("pop = %q, want %q", got, "b")
	}
}

func TestBinaryKeyData(t *testing.T) {
	lru := NewLRUQueue()
	k1 := []byte{0x00, 0xFF, 0x01}
	k2 := []byte{0x00, 0xFF, 0x02}

	lru.Push(k1, 10)
	lru.Push(k2, 20)

	if got := lru.GetCount(); got != 2 {
		t.Fatalf("GetCount() = %d, want 2", got)
	}
	if got := lru.PopLeastRecentlyUsed(); got != string(k1) {
		t.Fatalf("pop = %q, want %q", got, string(k1))
	}

	lru.Touch(k2)
	if got := lru.PopLeastRecentlyUsed(); got != string(k2) {
		t.Fatalf("pop = %q, want %q", got, string(k2))
	}
}

func TestCallerMutationAfterTouchDoesNotAffectQueue(t *testing.T) {
	lru := NewLRUQueue()
	key := []byte("abc")
	lru.Push(key, 1)

	key[0] = 'Z'
	lru.Touch(key) // Touch with mutated key ("Zbc") — should be a no-op

	if got := lru.PopLeastRecentlyUsed(); got != "abc" {
		t.Fatalf("pop = %q, want %q", got, "abc")
	}
}

func TestManyEntries(t *testing.T) {
	lru := NewLRUQueue()
	n := 1000
	totalSize := 0

	for i := 0; i < n; i++ {
		k := fmt.Sprintf("key-%04d", i)
		lru.Push([]byte(k), i+1)
		totalSize += i + 1
	}

	if got := lru.GetCount(); got != n {
		t.Fatalf("GetCount() = %d, want %d", got, n)
	}
	if got := lru.GetTotalSize(); got != totalSize {
		t.Fatalf("GetTotalSize() = %d, want %d", got, totalSize)
	}

	// FIFO order should be maintained.
	for i := 0; i < n; i++ {
		want := fmt.Sprintf("key-%04d", i)
		if got := lru.PopLeastRecentlyUsed(); got != want {
			t.Fatalf("pop %d = %q, want %q", i, got, want)
		}
	}

	if got := lru.GetCount(); got != 0 {
		t.Fatalf("GetCount() after drain = %d, want 0", got)
	}
	if got := lru.GetTotalSize(); got != 0 {
		t.Fatalf("GetTotalSize() after drain = %d, want 0", got)
	}
}

func TestPushUpdatedSizeThenPopVerifySizeAccounting(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 10)
	lru.Push([]byte("b"), 20)
	lru.Push([]byte("a"), 5) // decrease a's size from 10 to 5

	// total = 5 + 20 = 25
	if got := lru.GetTotalSize(); got != 25 {
		t.Fatalf("GetTotalSize() = %d, want 25", got)
	}

	// Pop "b" (it's the LRU since "a" was re-pushed to back).
	lru.PopLeastRecentlyUsed()
	if got := lru.GetTotalSize(); got != 5 {
		t.Fatalf("GetTotalSize() after popping b = %d, want 5", got)
	}

	lru.PopLeastRecentlyUsed()
	if got := lru.GetTotalSize(); got != 0 {
		t.Fatalf("GetTotalSize() after popping a = %d, want 0", got)
	}
}
