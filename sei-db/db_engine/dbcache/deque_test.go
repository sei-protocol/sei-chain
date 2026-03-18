package dbcache

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewDequeStartsEmpty(t *testing.T) {
	d := NewDeque[int]()

	require.Equal(t, 0, d.Len())
	require.True(t, d.IsEmpty())
}

// --- Constructor with capacity ---

func TestNewWithCapacityRoundsUpToPowerOf2(t *testing.T) {
	tests := []struct {
		requested int
		expected  int
	}{
		{0, minDequeCapacity},
		{1, minDequeCapacity},
		{7, minDequeCapacity},
		{8, minDequeCapacity},
		{9, 16},
		{10, 16},
		{16, 16},
		{17, 32},
		{100, 128},
		{1023, 1024},
		{1024, 1024},
		{1025, 2048},
	}
	for _, tt := range tests {
		d := NewDequeWithCapacity[int](tt.requested)
		require.Equal(t, tt.expected, len(d.data), "requested %d", tt.requested)
		require.Equal(t, tt.expected-1, d.mask, "requested %d", tt.requested)
	}
}

func TestNewWithCapacityIsFunctional(t *testing.T) {
	d := NewDequeWithCapacity[int](1000)
	require.Equal(t, 1024, len(d.data))

	for i := 0; i < 1000; i++ {
		d.PushBack(i)
	}
	require.Equal(t, 1000, d.Len())
	require.Equal(t, 1024, len(d.data))

	d.PushBack(1000)
	require.Equal(t, 1001, d.Len())
	require.Equal(t, 1024, len(d.data))

	for i := 1001; i < 1024; i++ {
		d.PushBack(i)
	}
	require.Equal(t, 1024, len(d.data))

	d.PushBack(9999)
	require.Equal(t, 2048, len(d.data))
	require.Equal(t, 2047, d.mask)
}

func TestDefaultConstructorMatchesMinCapacity(t *testing.T) {
	d := NewDeque[int]()
	require.Equal(t, minDequeCapacity, len(d.data))
	require.Equal(t, minDequeCapacity-1, d.mask)
}

func TestMaskStaysConsistentThroughGrowth(t *testing.T) {
	d := NewDeque[int]()

	for i := 0; i < 100; i++ {
		d.PushBack(i)
		require.Equal(t, len(d.data)-1, d.mask, "after push %d", i)
	}
}

// --- PushBack ---

func TestPushBackSingleElement(t *testing.T) {
	d := NewDeque[string]()
	d.PushBack("a")

	require.Equal(t, 1, d.Len())
	require.False(t, d.IsEmpty())
	require.Equal(t, "a", d.PeekFront())
	require.Equal(t, "a", d.PeekBack())
}

func TestPushBackMultipleElements(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 5; i++ {
		d.PushBack(i)
	}

	require.Equal(t, 5, d.Len())
	require.Equal(t, 0, d.PeekFront())
	require.Equal(t, 4, d.PeekBack())
}

// --- PushFront ---

func TestPushFrontSingleElement(t *testing.T) {
	d := NewDeque[string]()
	d.PushFront("x")

	require.Equal(t, 1, d.Len())
	require.Equal(t, "x", d.PeekFront())
	require.Equal(t, "x", d.PeekBack())
}

func TestPushFrontMultipleElements(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 5; i++ {
		d.PushFront(i)
	}

	require.Equal(t, 5, d.Len())
	require.Equal(t, 4, d.PeekFront())
	require.Equal(t, 0, d.PeekBack())
}

func TestMixedPushFrontAndBack(t *testing.T) {
	d := NewDeque[string]()
	d.PushBack("b")
	d.PushFront("a")
	d.PushBack("c")
	d.PushFront("z")

	require.Equal(t, 4, d.Len())
	require.Equal(t, "z", d.PeekFront())
	require.Equal(t, "c", d.PeekBack())

	var got []string
	for _, v := range d.Forward() {
		got = append(got, v)
	}
	require.Equal(t, []string{"z", "a", "b", "c"}, got)
}

// --- Growth ---

func TestGrowthDoublesCapacity(t *testing.T) {
	d := NewDeque[int]()
	initial := len(d.data)
	require.Equal(t, minDequeCapacity, initial)

	for i := 0; i < minDequeCapacity; i++ {
		d.PushBack(i)
	}
	require.Equal(t, minDequeCapacity, len(d.data))

	d.PushBack(999)
	require.Equal(t, minDequeCapacity*2, len(d.data))

	for i := 0; i < minDequeCapacity; i++ {
		v, ok := d.Get(i)
		require.True(t, ok)
		require.Equal(t, i, v)
	}
	v, ok := d.Get(minDequeCapacity)
	require.True(t, ok)
	require.Equal(t, 999, v)
}

func TestCapacityNeverShrinks(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 20; i++ {
		d.PushBack(i)
	}
	capAfterGrowth := len(d.data)

	for i := 0; i < 20; i++ {
		d.PopFront()
	}
	require.Equal(t, 0, d.Len())
	require.Equal(t, capAfterGrowth, len(d.data))
}

func TestGrowthWithWrappedBuffer(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 6; i++ {
		d.PushBack(i)
	}
	for i := 0; i < 4; i++ {
		d.PopFront()
	}
	// firstIndex is now 4, size is 2, elements [4, 5]
	// Fill to capacity to force a grow while wrapped
	for i := 10; i < 10+minDequeCapacity-2; i++ {
		d.PushBack(i)
	}
	require.Equal(t, minDequeCapacity, d.Len())

	d.PushBack(99)
	require.Equal(t, minDequeCapacity+1, d.Len())
	require.Equal(t, 4, d.PeekFront())
	require.Equal(t, 99, d.PeekBack())
}

// --- TryPopFront / PopFront ---

func TestTryPopFrontEmpty(t *testing.T) {
	d := NewDeque[int]()
	v, ok := d.TryPopFront()
	require.False(t, ok)
	require.Equal(t, 0, v)
}

func TestPopFrontPanicsOnEmpty(t *testing.T) {
	d := NewDeque[int]()
	require.Panics(t, func() { d.PopFront() })
}

func TestPopFrontPanicsAfterDrain(t *testing.T) {
	d := NewDeque[int]()
	d.PushBack(1)
	d.PopFront()
	require.Panics(t, func() { d.PopFront() })
}

func TestTryPopFrontSingleElement(t *testing.T) {
	d := NewDeque[string]()
	d.PushBack("only")

	v, ok := d.TryPopFront()
	require.True(t, ok)
	require.Equal(t, "only", v)
	require.True(t, d.IsEmpty())
}

func TestPopFrontFIFOOrder(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 5; i++ {
		d.PushBack(i)
	}
	for i := 0; i < 5; i++ {
		require.Equal(t, i, d.PopFront())
	}
}

// --- TryPopBack / PopBack ---

func TestTryPopBackEmpty(t *testing.T) {
	d := NewDeque[int]()
	v, ok := d.TryPopBack()
	require.False(t, ok)
	require.Equal(t, 0, v)
}

func TestPopBackPanicsOnEmpty(t *testing.T) {
	d := NewDeque[int]()
	require.Panics(t, func() { d.PopBack() })
}

func TestPopBackPanicsAfterDrain(t *testing.T) {
	d := NewDeque[int]()
	d.PushBack(1)
	d.PopBack()
	require.Panics(t, func() { d.PopBack() })
}

func TestTryPopBackSingleElement(t *testing.T) {
	d := NewDeque[string]()
	d.PushBack("only")

	v, ok := d.TryPopBack()
	require.True(t, ok)
	require.Equal(t, "only", v)
	require.True(t, d.IsEmpty())
}

func TestPopBackLIFOOrder(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 5; i++ {
		d.PushBack(i)
	}
	for i := 4; i >= 0; i-- {
		require.Equal(t, i, d.PopBack())
	}
}

// --- TryPeekFront / PeekFront ---

func TestTryPeekFrontEmpty(t *testing.T) {
	d := NewDeque[int]()
	v, ok := d.TryPeekFront()
	require.False(t, ok)
	require.Equal(t, 0, v)
}

func TestPeekFrontPanicsOnEmpty(t *testing.T) {
	d := NewDeque[int]()
	require.Panics(t, func() { d.PeekFront() })
}

func TestPeekFrontDoesNotRemove(t *testing.T) {
	d := NewDeque[int]()
	d.PushBack(42)

	require.Equal(t, 42, d.PeekFront())
	require.Equal(t, 42, d.PeekFront())
	require.Equal(t, 1, d.Len())
}

// --- TryPeekBack / PeekBack ---

func TestTryPeekBackEmpty(t *testing.T) {
	d := NewDeque[int]()
	v, ok := d.TryPeekBack()
	require.False(t, ok)
	require.Equal(t, 0, v)
}

func TestPeekBackPanicsOnEmpty(t *testing.T) {
	d := NewDeque[int]()
	require.Panics(t, func() { d.PeekBack() })
}

func TestPeekBackDoesNotRemove(t *testing.T) {
	d := NewDeque[int]()
	d.PushBack(42)

	require.Equal(t, 42, d.PeekBack())
	require.Equal(t, 42, d.PeekBack())
	require.Equal(t, 1, d.Len())
}

// --- Get ---

func TestGetPositiveIndex(t *testing.T) {
	d := NewDeque[string]()
	d.PushBack("a")
	d.PushBack("b")
	d.PushBack("c")

	for i, want := range []string{"a", "b", "c"} {
		v, ok := d.Get(i)
		require.True(t, ok)
		require.Equal(t, want, v)
	}
}

func TestGetNegativeIndex(t *testing.T) {
	d := NewDeque[string]()
	d.PushBack("a")
	d.PushBack("b")
	d.PushBack("c")

	v, ok := d.Get(-1)
	require.True(t, ok)
	require.Equal(t, "c", v)

	v, ok = d.Get(-2)
	require.True(t, ok)
	require.Equal(t, "b", v)

	v, ok = d.Get(-3)
	require.True(t, ok)
	require.Equal(t, "a", v)
}

func TestGetOutOfBounds(t *testing.T) {
	d := NewDeque[int]()
	d.PushBack(1)
	d.PushBack(2)

	_, ok := d.Get(2)
	require.False(t, ok)

	_, ok = d.Get(-3)
	require.False(t, ok)

	_, ok = d.Get(100)
	require.False(t, ok)
}

func TestGetOnEmptyDeque(t *testing.T) {
	d := NewDeque[int]()

	_, ok := d.Get(0)
	require.False(t, ok)

	_, ok = d.Get(-1)
	require.False(t, ok)
}

func TestGetWithWrappedBuffer(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 6; i++ {
		d.PushBack(i)
	}
	for i := 0; i < 4; i++ {
		d.PopFront()
	}
	d.PushBack(10)
	d.PushBack(11)

	// Logical: [4, 5, 10, 11]
	expected := []int{4, 5, 10, 11}
	for i, want := range expected {
		v, ok := d.Get(i)
		require.True(t, ok)
		require.Equal(t, want, v)
	}
}

// --- Set ---

func TestSetPositiveIndex(t *testing.T) {
	d := NewDeque[int]()
	d.PushBack(0)
	d.PushBack(0)
	d.PushBack(0)

	d.Set(1, 99)

	v, ok := d.Get(1)
	require.True(t, ok)
	require.Equal(t, 99, v)
}

func TestSetNegativeIndex(t *testing.T) {
	d := NewDeque[int]()
	d.PushBack(0)
	d.PushBack(0)
	d.PushBack(0)

	d.Set(-1, 77)

	v, ok := d.Get(2)
	require.True(t, ok)
	require.Equal(t, 77, v)
}

func TestSetOutOfBoundsIsNoop(t *testing.T) {
	d := NewDeque[int]()
	d.PushBack(1)

	d.Set(5, 99)
	d.Set(-5, 99)

	v, ok := d.Get(0)
	require.True(t, ok)
	require.Equal(t, 1, v)
	require.Equal(t, 1, d.Len())
}

func TestSetOnEmptyDequeIsNoop(t *testing.T) {
	d := NewDeque[int]()
	d.Set(0, 99)
	require.True(t, d.IsEmpty())
}

// --- Clear ---

func TestClearEmptyDeque(t *testing.T) {
	d := NewDeque[int]()
	d.Clear()

	require.True(t, d.IsEmpty())
	require.Equal(t, 0, d.Len())
}

func TestClearNonEmptyDeque(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 10; i++ {
		d.PushBack(i)
	}

	d.Clear()

	require.True(t, d.IsEmpty())
	require.Equal(t, 0, d.Len())
}

func TestClearWrappedBufferZeroesAllSlots(t *testing.T) {
	d := NewDeque[*int]()
	for i := 0; i < 5; i++ {
		v := new(int)
		*v = i
		d.PushBack(v)
	}
	for i := 0; i < 4; i++ {
		d.PopFront()
	}
	// firstIndex is now 4, one element remains at slot 4
	// Push more to wrap around past end of slice
	for i := 10; i < 14; i++ {
		v := new(int)
		*v = i
		d.PushBack(v)
	}
	// Elements span across the wrap boundary
	require.True(t, d.firstIndex+d.size > len(d.data))

	d.Clear()

	for i := 0; i < len(d.data); i++ {
		require.Nil(t, d.data[i], "slot %d not zeroed", i)
	}
}

func TestClearAllowsReuse(t *testing.T) {
	d := NewDeque[int]()
	d.PushBack(1)
	d.PushBack(2)
	d.Clear()

	d.PushBack(10)
	d.PushBack(20)

	require.Equal(t, 2, d.Len())
	require.Equal(t, 10, d.PeekFront())
	require.Equal(t, 20, d.PeekBack())
}

// --- GC zeroing ---

func TestPopFrontZeroesSlotForGC(t *testing.T) {
	d := NewDeque[*int]()
	x := new(int)
	*x = 42
	d.PushBack(x)

	slot := d.firstIndex
	d.PopFront()

	require.Nil(t, d.data[slot])
}

func TestPopBackZeroesSlotForGC(t *testing.T) {
	d := NewDeque[*int]()
	x := new(int)
	*x = 42
	d.PushBack(x)

	slot := (d.firstIndex + d.size - 1) % len(d.data)
	d.PopBack()

	require.Nil(t, d.data[slot])
}

func TestClearZeroesAllSlotsForGC(t *testing.T) {
	d := NewDeque[*int]()
	for i := 0; i < 5; i++ {
		v := new(int)
		*v = i
		d.PushBack(v)
	}

	d.Clear()

	for i := 0; i < len(d.data); i++ {
		require.Nil(t, d.data[i])
	}
}

// --- Wrap-around ---

func TestWrapAroundPushFront(t *testing.T) {
	d := NewDeque[int]()
	// firstIndex starts at 0; pushing front wraps to end of backing slice
	d.PushFront(1)
	d.PushFront(2)
	d.PushFront(3)

	require.Equal(t, 3, d.PeekFront())
	require.Equal(t, 1, d.PeekBack())

	require.Equal(t, 3, d.PopFront())
	require.Equal(t, 2, d.PopFront())
	require.Equal(t, 1, d.PopFront())
}

func TestWrapAroundInterleavedOps(t *testing.T) {
	d := NewDeque[int]()

	// Fill half from back, pop from front to advance firstIndex
	for i := 0; i < 6; i++ {
		d.PushBack(i)
	}
	for i := 0; i < 6; i++ {
		require.Equal(t, i, d.PopFront())
	}

	// Now firstIndex is 6, push to wrap around
	for i := 100; i < 108; i++ {
		d.PushBack(i)
	}
	require.Equal(t, 8, d.Len())
	for i := 100; i < 108; i++ {
		require.Equal(t, i, d.PopFront())
	}
}

// --- Forward iterator ---

func TestForwardEmptyDeque(t *testing.T) {
	d := NewDeque[int]()

	count := 0
	for range d.Forward() {
		count++
	}
	require.Equal(t, 0, count)
}

func TestForwardOrder(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 5; i++ {
		d.PushBack(i * 10)
	}

	var indices []int
	var values []int
	for i, v := range d.Forward() {
		indices = append(indices, i)
		values = append(values, v)
	}

	require.Equal(t, []int{0, 1, 2, 3, 4}, indices)
	require.Equal(t, []int{0, 10, 20, 30, 40}, values)
}

func TestForwardEarlyBreak(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 10; i++ {
		d.PushBack(i)
	}

	count := 0
	for range d.Forward() {
		count++
		if count == 3 {
			break
		}
	}
	require.Equal(t, 3, count)
}

func TestForwardWithWrappedBuffer(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 6; i++ {
		d.PushBack(i)
	}
	for i := 0; i < 5; i++ {
		d.PopFront()
	}
	for i := 10; i < 15; i++ {
		d.PushBack(i)
	}

	// Logical: [5, 10, 11, 12, 13, 14]
	var values []int
	for _, v := range d.Forward() {
		values = append(values, v)
	}
	require.Equal(t, []int{5, 10, 11, 12, 13, 14}, values)
}

// --- Backward iterator ---

func TestBackwardEmptyDeque(t *testing.T) {
	d := NewDeque[int]()

	count := 0
	for range d.Backward() {
		count++
	}
	require.Equal(t, 0, count)
}

func TestBackwardOrder(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 5; i++ {
		d.PushBack(i * 10)
	}

	var indices []int
	var values []int
	for i, v := range d.Backward() {
		indices = append(indices, i)
		values = append(values, v)
	}

	require.Equal(t, []int{4, 3, 2, 1, 0}, indices)
	require.Equal(t, []int{40, 30, 20, 10, 0}, values)
}

func TestBackwardEarlyBreak(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 10; i++ {
		d.PushBack(i)
	}

	count := 0
	for range d.Backward() {
		count++
		if count == 3 {
			break
		}
	}
	require.Equal(t, 3, count)
}

func TestBackwardWithWrappedBuffer(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 6; i++ {
		d.PushBack(i)
	}
	for i := 0; i < 5; i++ {
		d.PopFront()
	}
	for i := 10; i < 15; i++ {
		d.PushBack(i)
	}

	var values []int
	for _, v := range d.Backward() {
		values = append(values, v)
	}
	require.Equal(t, []int{14, 13, 12, 11, 10, 5}, values)
}

// --- Interleaved push/pop ---

func TestInterleavedPushPopFrontBack(t *testing.T) {
	d := NewDeque[int]()

	d.PushBack(1)
	d.PushBack(2)
	d.PushFront(0)

	require.Equal(t, 0, d.PopFront())
	require.Equal(t, 2, d.PopBack())

	d.PushBack(3)
	d.PushFront(-1)

	// Logical: [-1, 1, 3]
	require.Equal(t, 3, d.Len())
	require.Equal(t, -1, d.PopFront())
	require.Equal(t, 3, d.PopBack())
	require.Equal(t, 1, d.PopFront())
	require.True(t, d.IsEmpty())
}

func TestPushAfterFullDrain(t *testing.T) {
	d := NewDeque[int]()
	d.PushBack(1)
	d.PushBack(2)
	d.PopFront()
	d.PopFront()

	d.PushBack(10)
	d.PushFront(9)

	require.Equal(t, 2, d.Len())
	require.Equal(t, 9, d.PeekFront())
	require.Equal(t, 10, d.PeekBack())
}

// --- Use as stack (LIFO) ---

func TestUseAsStack(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 5; i++ {
		d.PushBack(i)
	}
	for i := 4; i >= 0; i-- {
		require.Equal(t, i, d.PopBack())
	}
	require.True(t, d.IsEmpty())
}

// --- Use as queue (FIFO) ---

func TestUseAsQueue(t *testing.T) {
	d := NewDeque[int]()
	for i := 0; i < 5; i++ {
		d.PushBack(i)
	}
	for i := 0; i < 5; i++ {
		require.Equal(t, i, d.PopFront())
	}
	require.True(t, d.IsEmpty())
}

// --- Large stress test ---

func TestLargeNumberOfElements(t *testing.T) {
	d := NewDeque[int]()
	n := 10_000

	for i := 0; i < n; i++ {
		d.PushBack(i)
	}
	require.Equal(t, n, d.Len())

	for i := 0; i < n; i++ {
		v, ok := d.Get(i)
		require.True(t, ok)
		require.Equal(t, i, v)
	}

	for i := 0; i < n/2; i++ {
		require.Equal(t, i, d.PopFront())
	}
	for i := n - 1; i >= n/2; i-- {
		require.Equal(t, i, d.PopBack())
	}
	require.True(t, d.IsEmpty())
}

// --- Alternating push-front/pop-back stress ---

func TestAlternatingPushFrontPopBack(t *testing.T) {
	d := NewDeque[int]()

	for i := 0; i < 100; i++ {
		d.PushFront(i)
		if i%3 == 0 && !d.IsEmpty() {
			d.PopBack()
		}
	}

	prev := d.PeekFront()
	d.PopFront()
	for !d.IsEmpty() {
		cur := d.PopFront()
		require.Greater(t, prev, cur)
		prev = cur
	}
}

// --- Single element edge cases ---

func TestSingleElementPopFrontAndPopBackEquivalent(t *testing.T) {
	d1 := NewDeque[int]()
	d1.PushBack(42)
	v1 := d1.PopFront()

	d2 := NewDeque[int]()
	d2.PushBack(42)
	v2 := d2.PopBack()

	require.Equal(t, v1, v2)
}

func TestSingleElementPeekFrontAndPeekBackEquivalent(t *testing.T) {
	d := NewDeque[int]()
	d.PushBack(42)

	require.Equal(t, d.PeekFront(), d.PeekBack())
}

func TestSingleElementGetZeroAndNegativeOne(t *testing.T) {
	d := NewDeque[int]()
	d.PushBack(42)

	v0, ok0 := d.Get(0)
	vn1, okn1 := d.Get(-1)

	require.True(t, ok0)
	require.True(t, okn1)
	require.Equal(t, v0, vn1)
}
