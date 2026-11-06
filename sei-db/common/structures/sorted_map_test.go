package structures

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// sortedMapKeys returns the keys of m in the order Forward() yields them.
func sortedMapKeys(m *SortedMap[int, string]) []int {
	keys := make([]int, 0, m.Len())
	for key := range m.Forward() {
		keys = append(keys, key)
	}
	return keys
}

func TestNewSortedMapStartsEmpty(t *testing.T) {
	m := NewSortedMap[int, string]()

	require.Equal(t, 0, m.Len())
	_, _, ok := m.PeekFront()
	require.False(t, ok)
	_, _, ok = m.PeekBack()
	require.False(t, ok)
	require.Empty(t, sortedMapKeys(m))
}

// --- Put ---

func TestPutAscending(t *testing.T) {
	m := NewSortedMap[int, string]()
	for i := 0; i < 5; i++ {
		m.Put(i, "v")
	}

	require.Equal(t, []int{0, 1, 2, 3, 4}, sortedMapKeys(m))
}

func TestPutDescending(t *testing.T) {
	m := NewSortedMap[int, string]()
	for i := 4; i >= 0; i-- {
		m.Put(i, "v")
	}

	require.Equal(t, []int{0, 1, 2, 3, 4}, sortedMapKeys(m))
}

func TestPutMiddle(t *testing.T) {
	m := NewSortedMap[int, string]()
	m.Put(0, "a")
	m.Put(10, "b")
	m.Put(5, "c")
	m.Put(7, "d")
	m.Put(3, "e")

	require.Equal(t, []int{0, 3, 5, 7, 10}, sortedMapKeys(m))
}

func TestPutReplacesValue(t *testing.T) {
	m := NewSortedMap[int, string]()
	m.Put(1, "a")
	m.Put(2, "b")
	m.Put(1, "c")

	require.Equal(t, 2, m.Len())
	require.Equal(t, []int{1, 2}, sortedMapKeys(m))
	value, ok := m.Get(1)
	require.True(t, ok)
	require.Equal(t, "c", value)
}

// --- Get ---

func TestGet(t *testing.T) {
	m := NewSortedMap[int, string]()
	m.Put(1, "a")
	m.Put(2, "b")

	value, ok := m.Get(2)
	require.True(t, ok)
	require.Equal(t, "b", value)

	_, ok = m.Get(3)
	require.False(t, ok)
}

// --- Remove ---

func TestRemoveFrontMiddleAndBack(t *testing.T) {
	m := NewSortedMap[int, string]()
	for i := 0; i < 5; i++ {
		m.Put(i, "v")
	}

	value, ok := m.Remove(2)
	require.True(t, ok)
	require.Equal(t, "v", value)
	require.Equal(t, []int{0, 1, 3, 4}, sortedMapKeys(m))

	_, ok = m.Remove(0)
	require.True(t, ok)
	require.Equal(t, []int{1, 3, 4}, sortedMapKeys(m))

	_, ok = m.Remove(4)
	require.True(t, ok)
	require.Equal(t, []int{1, 3}, sortedMapKeys(m))

	// Puts at both ends still land correctly after removals at both ends.
	m.Put(5, "v")
	m.Put(-1, "v")
	require.Equal(t, []int{-1, 1, 3, 5}, sortedMapKeys(m))
}

func TestRemoveMissing(t *testing.T) {
	m := NewSortedMap[int, string]()
	m.Put(1, "a")

	_, ok := m.Remove(2)
	require.False(t, ok)
	require.Equal(t, 1, m.Len())
}

func TestRemoveLastEntry(t *testing.T) {
	m := NewSortedMap[int, string]()
	m.Put(1, "a")

	_, ok := m.Remove(1)
	require.True(t, ok)
	require.Equal(t, 0, m.Len())
	_, _, ok = m.PeekFront()
	require.False(t, ok)

	m.Put(2, "b")
	require.Equal(t, []int{2}, sortedMapKeys(m))
}

// --- PeekFront / PopFront ---

func TestPeekFrontDoesNotRemoveEntry(t *testing.T) {
	m := NewSortedMap[int, string]()
	m.Put(2, "b")
	m.Put(1, "a")

	key, value, ok := m.PeekFront()
	require.True(t, ok)
	require.Equal(t, 1, key)
	require.Equal(t, "a", value)
	require.Equal(t, 2, m.Len())
}

func TestPopFrontInKeyOrder(t *testing.T) {
	m := NewSortedMap[int, string]()
	m.Put(3, "c")
	m.Put(1, "a")
	m.Put(2, "b")

	for want := 1; want <= 3; want++ {
		key, _, ok := m.PopFront()
		require.True(t, ok)
		require.Equal(t, want, key)
	}
	_, _, ok := m.PopFront()
	require.False(t, ok)
	require.Equal(t, 0, m.Len())
}

// --- PeekBack / PopBack ---

func TestPeekBackDoesNotRemoveEntry(t *testing.T) {
	m := NewSortedMap[int, string]()
	m.Put(2, "b")
	m.Put(1, "a")

	key, value, ok := m.PeekBack()
	require.True(t, ok)
	require.Equal(t, 2, key)
	require.Equal(t, "b", value)
	require.Equal(t, 2, m.Len())
}

func TestPopBackInReverseKeyOrder(t *testing.T) {
	m := NewSortedMap[int, string]()
	m.Put(1, "a")
	m.Put(3, "c")
	m.Put(2, "b")

	for want := 3; want >= 1; want-- {
		key, _, ok := m.PopBack()
		require.True(t, ok)
		require.Equal(t, want, key)
	}
	_, _, ok := m.PopBack()
	require.False(t, ok)
	require.Equal(t, 0, m.Len())

	// The map is usable again once emptied from the back.
	m.Put(5, "e")
	require.Equal(t, []int{5}, sortedMapKeys(m))
}

// --- Forward ---

func TestForwardStopsEarly(t *testing.T) {
	m := NewSortedMap[int, string]()
	for i := 0; i < 5; i++ {
		m.Put(i, "v")
	}

	var keys []int
	for key := range m.Forward() {
		if key == 2 {
			break
		}
		keys = append(keys, key)
	}
	require.Equal(t, []int{0, 1}, keys)
}

// --- Random operations ---

func TestRandomOperationsMatchReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	m := NewSortedMap[int, string]()
	reference := make(map[int]string)

	for i := 0; i < 10_000; i++ {
		key := rng.IntN(200)
		switch rng.IntN(3) {
		case 0, 1:
			value := string(rune('a' + rng.IntN(26)))
			m.Put(key, value)
			reference[key] = value
		case 2:
			_, ok := m.Remove(key)
			_, want := reference[key]
			require.Equal(t, want, ok)
			delete(reference, key)
		}

		require.Equal(t, len(reference), m.Len())
		if i%100 == 0 {
			keys := make([]int, 0, len(reference))
			for key := range reference {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			require.Equal(t, keys, sortedMapKeys(m))
			for key, want := range reference {
				value, ok := m.Get(key)
				require.True(t, ok)
				require.Equal(t, want, value)
			}
		}
	}
}
