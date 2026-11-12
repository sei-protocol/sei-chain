package memiavl

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// newSnapshotTree returns a tree backed by a snapshot holding leaves keys with values of valueSize
// bytes, so leaf i of the snapshot is the i-th key in order.
func newSnapshotTree(t *testing.T, leaves, valueSize int) *Tree {
	t.Helper()
	tree := New(0)
	for i := range leaves {
		tree.Set(fmt.Appendf(nil, "key-%08d", i), bytes.Repeat([]byte{byte(i)}, valueSize))
	}
	_, _, err := tree.SaveVersion(true)
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, tree.WriteSnapshot(t.Context(), dir))
	snapshot, err := OpenSnapshot(dir, Options{})
	require.NoError(t, err)
	tree = NewFromSnapshot(snapshot, Options{ZeroCopy: true})
	t.Cleanup(func() { require.NoError(t, tree.Close()) })
	return tree
}

// snapshotSize returns the combined size in bytes of the snapshot's nodes, leaves and kvs files.
func snapshotSize(snapshot *Snapshot) uint64 {
	return uint64(len(snapshot.nodes) + len(snapshot.leaves) + len(snapshot.kvs))
}

// reachedWindows returns how many windows an iterator has reached, failing unless they are the first
// ones.
func reachedWindows(t *testing.T, windows *readAheadWindows) int {
	t.Helper()
	reached := 0
	for i := range windows.read {
		if windows.read[i].Load() {
			require.Equal(t, reached, i, "window %d was reached before window %d", i, reached)
			reached++
		}
	}
	return reached
}

func TestReadAheadReachesEachWindowWithTheFirstLeafAnIteratorReturnsFromIt(t *testing.T) {
	tree := newSnapshotTree(t, 1000, 100)
	unarmed := tree.Iterator(nil, nil, true)
	want := collectIter(unarmed)
	require.NoError(t, unarmed.Close())

	stop := tree.readAhead("test", snapshotSize(tree.snapshot)/5)
	defer stop()
	windows := tree.snapshot.readAhead.Load()
	require.NotNil(t, windows)
	require.Greater(t, len(windows.read), 3)

	iter := tree.Iterator(nil, nil, true)
	var got []pair
	for leaf := uint64(0); iter.Valid(); leaf++ {
		require.Equal(t, int(leaf/windows.windowLeaves)+1, reachedWindows(t, windows), "at leaf %d", leaf)
		got = append(got, pair{key: iter.Key(), value: iter.Value()})
		iter.Next()
	}
	require.NoError(t, iter.Close())
	require.Equal(t, want, got)
	require.Equal(t, len(windows.read), reachedWindows(t, windows))
}

func TestReadAheadServesOnlyAscendingIteratorsCreatedWhileArmed(t *testing.T) {
	treeWithoutSnapshot := New(0)
	treeWithoutSnapshot.readAhead("test", 1)()

	tree := newSnapshotTree(t, 1000, 100)
	stop := tree.readAhead("test", 1)
	windows := tree.snapshot.readAhead.Load()
	require.NotNil(t, windows)

	tree.readAhead("test", 1)()
	require.Same(t, windows, tree.snapshot.readAhead.Load(), "arming twice replaced or stopped the first read-ahead")

	descending := tree.Iterator(nil, nil, false)
	require.Len(t, collectIter(descending), 1000)
	require.NoError(t, descending.Close())
	require.Zero(t, reachedWindows(t, windows), "a descending iterator read ahead")

	stop()
	require.Nil(t, tree.snapshot.readAhead.Load())
	ascending := tree.Iterator(nil, nil, true)
	require.Len(t, collectIter(ascending), 1000)
	require.NoError(t, ascending.Close())
	require.Zero(t, reachedWindows(t, windows), "an iterator created after stop read ahead")
}
