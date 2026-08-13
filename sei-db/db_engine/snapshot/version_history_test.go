package snapshot

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// historyVersions returns the versions a history holds, oldest first.
func historyVersions(h versionHistory) []uint64 {
	versions := make([]uint64, 0, h.len())
	for i := 0; i < h.len(); i++ {
		versions = append(versions, h.get(i).version)
	}
	return versions
}

// newHistory starts a history the way setLocked does for a key new to the window.
func newHistory(version uint64, value string) versionHistory {
	return versionHistory{newest: versionedValue{version: version, value: []byte(value)}}
}

// A key written once holds exactly that value and allocates no overflow deque.
func TestVersionHistorySingleVersion(t *testing.T) {
	history := newHistory(3, "a")

	require.Equal(t, 1, history.len())
	require.Nil(t, history.older, "a single-version history must not allocate an overflow deque")
	require.Equal(t, uint64(3), history.oldest().version)
	require.Equal(t, uint64(3), history.newest.version)
	require.Equal(t, []byte("a"), history.get(0).value)
}

// Writing the same version twice replaces the value rather than recording the version twice.
func TestVersionHistoryRepeatWriteAtSameVersion(t *testing.T) {
	history := newHistory(3, "a")
	history = history.set(versionedValue{version: 3, value: []byte("b")})

	require.Equal(t, []uint64{3}, historyVersions(history))
	require.Equal(t, []byte("b"), history.newest.value)
	require.Nil(t, history.older, "replacing a value must not allocate an overflow deque")
}

// Writing at later versions keeps every value, oldest first.
func TestVersionHistoryOrdersVersionsOldestFirst(t *testing.T) {
	history := newHistory(1, "a")
	history = history.set(versionedValue{version: 2, value: []byte("b")})
	history = history.set(versionedValue{version: 4, value: []byte("d")})

	require.Equal(t, []uint64{1, 2, 4}, historyVersions(history))
	require.Equal(t, uint64(1), history.oldest().version)
	require.Equal(t, []byte("d"), history.newest.value)
	require.Equal(t, []byte("a"), history.get(0).value)
	require.Equal(t, []byte("b"), history.get(1).value)
}

// A repeat write at the newest version replaces it without disturbing older versions.
func TestVersionHistoryRepeatWriteKeepsOlderVersions(t *testing.T) {
	history := newHistory(1, "a")
	history = history.set(versionedValue{version: 2, value: []byte("b")})
	history = history.set(versionedValue{version: 2, value: []byte("c")})

	require.Equal(t, []uint64{1, 2}, historyVersions(history))
	require.Equal(t, []byte("a"), history.get(0).value)
	require.Equal(t, []byte("c"), history.newest.value)
}

func TestVersionHistoryDropOlderThan(t *testing.T) {
	t.Run("drops only versions below the cut", func(t *testing.T) {
		history := newHistory(1, "a")
		history = history.set(versionedValue{version: 2, value: []byte("b")})
		history = history.set(versionedValue{version: 3, value: []byte("c")})

		history, remaining := history.dropOlderThan(3)
		require.True(t, remaining)
		require.Equal(t, []uint64{3}, historyVersions(history))
		require.Equal(t, []byte("c"), history.newest.value)
	})

	t.Run("keeps the newest value when it is at the cut", func(t *testing.T) {
		history := newHistory(5, "a")

		history, remaining := history.dropOlderThan(5)
		require.True(t, remaining)
		require.Equal(t, []uint64{5}, historyVersions(history))
	})

	t.Run("reports nothing remaining when every version is below the cut", func(t *testing.T) {
		history := newHistory(1, "a")
		history = history.set(versionedValue{version: 2, value: []byte("b")})

		_, remaining := history.dropOlderThan(3)
		require.False(t, remaining, "a fully retired history must be removed from versionedData")
	})

	t.Run("keeps a newer value even when older ones are dropped", func(t *testing.T) {
		history := newHistory(1, "a")
		history = history.set(versionedValue{version: 9, value: []byte("i")})

		history, remaining := history.dropOlderThan(5)
		require.True(t, remaining)
		require.Equal(t, []uint64{9}, historyVersions(history))
		require.Equal(t, []byte("i"), history.newest.value)
	})
}

// A deleted value is recorded as a nil-valued tombstone, which must survive as a distinct entry
// rather than being mistaken for an absent one.
func TestVersionHistoryKeepsTombstones(t *testing.T) {
	history := newHistory(1, "a")
	history = history.set(versionedValue{version: 2, value: nil})

	require.Equal(t, []uint64{1, 2}, historyVersions(history))
	require.Nil(t, history.newest.value)
	require.Equal(t, 2, history.len())
}
