package memiavl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// TestSnapshotLeaf tests the Leaf method
func TestSnapshotLeaf(t *testing.T) {
	tree := New(0)
	for _, changes := range ChangeSets {
		tree.ApplyChangeSet(changes)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()
	require.NoError(t, tree.WriteSnapshot(context.Background(), snapshotDir))

	opts := Options{}
	opts.FillDefaults()
	snapshot, err := OpenSnapshot(snapshotDir, opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, snapshot.Close()) })

	// Test Leaf method
	if snapshot.leavesLen() > 0 {
		leaf := snapshot.Leaf(0)
		require.NotNil(t, leaf)
	}
}

// TestSnapshotScanNodes tests the ScanNodes method
func TestSnapshotScanNodes(t *testing.T) {
	tree := New(0)
	for _, changes := range ChangeSets {
		tree.ApplyChangeSet(changes)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()
	require.NoError(t, tree.WriteSnapshot(context.Background(), snapshotDir))

	opts := Options{}
	opts.FillDefaults()
	snapshot, err := OpenSnapshot(snapshotDir, opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, snapshot.Close()) })

	// Test ScanNodes
	count := 0
	err = snapshot.ScanNodes(func(node PersistedNode) error {
		count++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, snapshot.leavesLen()+snapshot.nodesLen(), count)
}

// TestSnapshotKey tests the Key method
func TestSnapshotKey(t *testing.T) {
	tree := New(0)
	for _, changes := range ChangeSets {
		tree.ApplyChangeSet(changes)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()
	require.NoError(t, tree.WriteSnapshot(context.Background(), snapshotDir))

	opts := Options{}
	opts.FillDefaults()
	snapshot, err := OpenSnapshot(snapshotDir, opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, snapshot.Close()) })

	// Test Key method via scanning leaves
	if snapshot.leavesLen() > 0 {
		leaf := snapshot.leavesLayout.Leaf(0)
		key := snapshot.Key(leaf.KeyOffset())
		require.NotNil(t, key)
	}
}

// TestSnapshotRootNodeEmpty tests RootNode on empty snapshot
func TestSnapshotRootNodeEmpty(t *testing.T) {
	snapshot := NewEmptySnapshot(0)

	// Should panic on empty snapshot
	defer func() {
		r := recover()
		require.NotNil(t, r)
	}()

	snapshot.RootNode()
}

// TestSnapshotRootHashEmpty tests RootHash on empty snapshot
func TestSnapshotRootHashEmpty(t *testing.T) {
	snapshot := NewEmptySnapshot(0)
	hash := snapshot.RootHash()
	require.Equal(t, emptyHash, hash)
}

// TestPrefetchSnapshot tests the prefetch functionality
func TestPrefetchSnapshot(t *testing.T) {
	tree := New(0)
	for _, changes := range ChangeSets {
		tree.ApplyChangeSet(changes)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()
	require.NoError(t, tree.WriteSnapshot(context.Background(), snapshotDir))

	// Test with prefetch enabled
	opts := Options{}
	opts.FillDefaults()
	opts.SnapshotPrefetchThreshold = 0.0 // Always prefetch for testing

	snapshot, err := OpenSnapshot(snapshotDir, opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, snapshot.Close()) })

	require.NotNil(t, snapshot)
}

// TestShouldPreloadTree tests the tree preload selection logic
func TestShouldPreloadTree(t *testing.T) {
	// Active trees should be preloaded
	require.True(t, shouldPreloadTree("evm"))
	require.True(t, shouldPreloadTree("bank"))
	require.True(t, shouldPreloadTree("acc"))
	require.True(t, shouldPreloadTree("wasm"))

	// Other trees should not be preloaded
	require.False(t, shouldPreloadTree("other"))
	require.False(t, shouldPreloadTree("test"))
}

// TestWriterLoopErrors tests error handling in writer loops
func TestWriterLoopErrors(t *testing.T) {
	// This test verifies that writer loops handle errors correctly
	// by writing to an invalid path
	tree := New(0)
	for i := 0; i < 10; i++ {
		tree.Set([]byte("key"+string(rune(i))), []byte("value"+string(rune(i))))
	}
	_, _, err := tree.SaveVersion(true)
	require.NoError(t, err)

	// Try to write to invalid directory
	err = tree.WriteSnapshot(context.Background(), "/invalid/path/snapshot")
	require.Error(t, err)
}

// TestImportCancellation tests import with context cancellation
func TestImportCancellation(t *testing.T) {
	tree := New(0)
	for _, changes := range ChangeSets {
		tree.ApplyChangeSet(changes)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()
	require.NoError(t, tree.WriteSnapshot(context.Background(), snapshotDir))

	opts := Options{}
	opts.FillDefaults()
	snapshot, err := OpenSnapshot(snapshotDir, opts)
	require.NoError(t, err)

	ch := make(chan *types.SnapshotNode, 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure context cleanup even if test fails early

	// Start exporting
	go func() {
		defer close(ch)
		exporter := snapshot.Export()
		count := 0
		for {
			node, err := exporter.Next()
			if err != nil {
				break
			}
			ch <- node
			count++
			// Cancel after a few nodes
			if count > 5 {
				cancel()
				break
			}
		}
	}()

	snapshotDir2 := t.TempDir()
	err = doImport(ctx, snapshotDir2, tree.Version(), ch)
	// Should handle cancellation
	if err != nil {
		require.Contains(t, err.Error(), "context")
	}
}
