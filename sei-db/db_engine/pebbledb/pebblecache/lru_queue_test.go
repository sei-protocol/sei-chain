package pebblecache

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLRUQueueIsolatesFromCallerMutation(t *testing.T) {
	lru := NewLRUQueue()

	key := []byte("a")
	lru.Push(key, 1)
	key[0] = 'z'

	require.Equal(t, "a", lru.PopLeastRecentlyUsed())
}

func TestNewLRUQueueStartsEmpty(t *testing.T) {
	lru := NewLRUQueue()

	require.Equal(t, 0, lru.GetCount())
	require.Equal(t, 0, lru.GetTotalSize())
}

func TestPopLeastRecentlyUsedPanicsOnEmptyQueue(t *testing.T) {
	lru := NewLRUQueue()
	require.Panics(t, func() { lru.PopLeastRecentlyUsed() })
}

func TestPopLeastRecentlyUsedPanicsAfterDrain(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("x"), 1)
	lru.PopLeastRecentlyUsed()

	require.Panics(t, func() { lru.PopLeastRecentlyUsed() })
}

func TestPushSingleElement(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("only"), 42)

	require.Equal(t, 1, lru.GetCount())
	require.Equal(t, 42, lru.GetTotalSize())
	require.Equal(t, "only", lru.PopLeastRecentlyUsed())
}

func TestPushDuplicateDecreasesSize(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("k"), 100)
	lru.Push([]byte("k"), 30)

	require.Equal(t, 1, lru.GetCount())
	require.Equal(t, 30, lru.GetTotalSize())
}

func TestPushDuplicateMovesToBack(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 1)
	lru.Push([]byte("b"), 1)
	lru.Push([]byte("c"), 1)

	// Re-push "a" — should move it behind "b" and "c"
	lru.Push([]byte("a"), 1)

	require.Equal(t, "b", lru.PopLeastRecentlyUsed())
	require.Equal(t, "c", lru.PopLeastRecentlyUsed())
	require.Equal(t, "a", lru.PopLeastRecentlyUsed())
}

func TestPushZeroSize(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("z"), 0)

	require.Equal(t, 1, lru.GetCount())
	require.Equal(t, 0, lru.GetTotalSize())
	require.Equal(t, "z", lru.PopLeastRecentlyUsed())
	require.Equal(t, 0, lru.GetTotalSize())
}

func TestPushEmptyKey(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte(""), 5)

	require.Equal(t, 1, lru.GetCount())
	require.Equal(t, "", lru.PopLeastRecentlyUsed())
}

func TestPushRepeatedUpdatesToSameKey(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("k"), 1)
	lru.Push([]byte("k"), 2)
	lru.Push([]byte("k"), 3)
	lru.Push([]byte("k"), 4)

	require.Equal(t, 1, lru.GetCount())
	require.Equal(t, 4, lru.GetTotalSize())
}

func TestTouchNonexistentKeyIsNoop(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 1)

	lru.Touch([]byte("missing"))

	require.Equal(t, 1, lru.GetCount())
	require.Equal(t, "a", lru.PopLeastRecentlyUsed())
}

func TestTouchOnEmptyQueueIsNoop(t *testing.T) {
	lru := NewLRUQueue()
	lru.Touch([]byte("ghost"))

	require.Equal(t, 0, lru.GetCount())
}

func TestTouchSingleElement(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("solo"), 10)
	lru.Touch([]byte("solo"))

	require.Equal(t, 1, lru.GetCount())
	require.Equal(t, "solo", lru.PopLeastRecentlyUsed())
}

func TestTouchDoesNotAffectSizeOrCount(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 3)
	lru.Push([]byte("b"), 7)

	lru.Touch([]byte("a"))

	require.Equal(t, 2, lru.GetCount())
	require.Equal(t, 10, lru.GetTotalSize())
}

func TestMultipleTouchesChangeOrder(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 1)
	lru.Push([]byte("b"), 1)
	lru.Push([]byte("c"), 1)

	// Order: a, b, c
	lru.Touch([]byte("a")) // Order: b, c, a
	lru.Touch([]byte("b")) // Order: c, a, b

	require.Equal(t, "c", lru.PopLeastRecentlyUsed())
	require.Equal(t, "a", lru.PopLeastRecentlyUsed())
	require.Equal(t, "b", lru.PopLeastRecentlyUsed())
}

func TestTouchAlreadyMostRecentIsNoop(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 1)
	lru.Push([]byte("b"), 1)

	lru.Touch([]byte("b")) // "b" is already at back

	require.Equal(t, "a", lru.PopLeastRecentlyUsed())
	require.Equal(t, "b", lru.PopLeastRecentlyUsed())
}

func TestPopDecrementsCountAndSize(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 10)
	lru.Push([]byte("b"), 20)
	lru.Push([]byte("c"), 30)

	lru.PopLeastRecentlyUsed()

	require.Equal(t, 2, lru.GetCount())
	require.Equal(t, 50, lru.GetTotalSize())

	lru.PopLeastRecentlyUsed()

	require.Equal(t, 1, lru.GetCount())
	require.Equal(t, 30, lru.GetTotalSize())
}

func TestPopFIFOOrderWithoutTouches(t *testing.T) {
	lru := NewLRUQueue()
	keys := []string{"first", "second", "third", "fourth"}
	for _, k := range keys {
		lru.Push([]byte(k), 1)
	}

	for _, want := range keys {
		require.Equal(t, want, lru.PopLeastRecentlyUsed())
	}
}

func TestPushAfterDrain(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 5)
	lru.PopLeastRecentlyUsed()

	lru.Push([]byte("x"), 10)
	lru.Push([]byte("y"), 20)

	require.Equal(t, 2, lru.GetCount())
	require.Equal(t, 30, lru.GetTotalSize())
	require.Equal(t, "x", lru.PopLeastRecentlyUsed())
}

func TestPushPreviouslyPoppedKey(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("recycled"), 5)
	lru.PopLeastRecentlyUsed()

	lru.Push([]byte("recycled"), 99)

	require.Equal(t, 1, lru.GetCount())
	require.Equal(t, 99, lru.GetTotalSize())
	require.Equal(t, "recycled", lru.PopLeastRecentlyUsed())
}

func TestInterleavedPushAndPop(t *testing.T) {
	lru := NewLRUQueue()

	lru.Push([]byte("a"), 1)
	lru.Push([]byte("b"), 2)

	require.Equal(t, "a", lru.PopLeastRecentlyUsed())

	lru.Push([]byte("c"), 3)

	require.Equal(t, 2, lru.GetCount())
	require.Equal(t, 5, lru.GetTotalSize())
	require.Equal(t, "b", lru.PopLeastRecentlyUsed())
	require.Equal(t, "c", lru.PopLeastRecentlyUsed())
}

func TestTouchThenPushSameKey(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 1)
	lru.Push([]byte("b"), 1)

	lru.Touch([]byte("a"))    // order: b, a
	lru.Push([]byte("a"), 50) // updates size, stays at back

	require.Equal(t, 2, lru.GetCount())
	require.Equal(t, 51, lru.GetTotalSize())
	require.Equal(t, "b", lru.PopLeastRecentlyUsed())
}

func TestBinaryKeyData(t *testing.T) {
	lru := NewLRUQueue()
	k1 := []byte{0x00, 0xFF, 0x01}
	k2 := []byte{0x00, 0xFF, 0x02}

	lru.Push(k1, 10)
	lru.Push(k2, 20)

	require.Equal(t, 2, lru.GetCount())
	require.Equal(t, string(k1), lru.PopLeastRecentlyUsed())

	lru.Touch(k2)
	require.Equal(t, string(k2), lru.PopLeastRecentlyUsed())
}

func TestCallerMutationAfterTouchDoesNotAffectQueue(t *testing.T) {
	lru := NewLRUQueue()
	key := []byte("abc")
	lru.Push(key, 1)

	key[0] = 'Z'
	lru.Touch(key) // Touch with mutated key ("Zbc") — should be a no-op

	require.Equal(t, "abc", lru.PopLeastRecentlyUsed())
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

	require.Equal(t, n, lru.GetCount())
	require.Equal(t, totalSize, lru.GetTotalSize())

	for i := 0; i < n; i++ {
		want := fmt.Sprintf("key-%04d", i)
		require.Equal(t, want, lru.PopLeastRecentlyUsed(), "pop %d", i)
	}

	require.Equal(t, 0, lru.GetCount())
	require.Equal(t, 0, lru.GetTotalSize())
}

func TestPushUpdatedSizeThenPopVerifySizeAccounting(t *testing.T) {
	lru := NewLRUQueue()
	lru.Push([]byte("a"), 10)
	lru.Push([]byte("b"), 20)
	lru.Push([]byte("a"), 5) // decrease a's size from 10 to 5

	require.Equal(t, 25, lru.GetTotalSize())

	// Pop "b" (it's the LRU since "a" was re-pushed to back).
	lru.PopLeastRecentlyUsed()
	require.Equal(t, 5, lru.GetTotalSize())

	lru.PopLeastRecentlyUsed()
	require.Equal(t, 0, lru.GetTotalSize())
}
