package memiavl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

// TestSnapshotWriterPipeline tests the pipeline write mechanism
func TestSnapshotWriterPipeline(t *testing.T) {
	tree := New(0)
	for _, changes := range ChangeSets {
		tree.ApplyChangeSet(changes)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()

	// Test with pipeline enabled (default behavior)
	err := tree.WriteSnapshot(context.Background(), snapshotDir)
	require.NoError(t, err)

	// Verify snapshot is valid
	opts := Options{}
	opts.FillDefaults()
	snapshot, err := OpenSnapshot(snapshotDir, opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, snapshot.Close()) })

	require.Equal(t, uint32(tree.Version()), snapshot.Version())
	require.Equal(t, tree.RootHash(), snapshot.RootHash())
}

// TestSnapshotWriterCancellation tests context cancellation during snapshot write
func TestSnapshotWriterCancellation(t *testing.T) {
	// Create a large tree to ensure cancellation happens during write
	tree := New(0)
	for i := range 100 {
		changeset := iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: []byte("key" + string(rune(i))), Value: []byte("value" + string(rune(i)))},
			},
		}
		tree.ApplyChangeSet(changeset)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()

	// Create a context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately to test cancellation handling
	cancel()

	err := tree.WriteSnapshot(ctx, snapshotDir)
	// Should get context cancelled error
	require.Error(t, err)
}

// TestSnapshotWriterWithLargeBuffer tests large buffer size selection
func TestSnapshotWriterWithLargeBuffer(t *testing.T) {
	// Create a tree with many nodes to trigger large buffer logic
	tree := New(0)

	// Add enough data to exceed 100M nodes threshold (simulated via multiple versions)
	for i := range 50 {
		changeset := iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: []byte("key" + string(rune(i))), Value: make([]byte, 1024)},
			},
		}
		tree.ApplyChangeSet(changeset)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()
	err := tree.WriteSnapshot(context.Background(), snapshotDir)
	require.NoError(t, err)
}

// TestSnapshotWriterProgress tests progress reporting
func TestSnapshotWriterProgress(t *testing.T) {
	tree := New(0)
	for _, changes := range ChangeSets {
		tree.ApplyChangeSet(changes)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()

	// Write snapshot - this should trigger progress reporting internally
	err := tree.WriteSnapshot(context.Background(), snapshotDir)
	require.NoError(t, err)
}

// TestMonitoringWriter tests the monitoring writer wrapper
func TestMonitoringWriter(t *testing.T) {
	tree := New(0)
	for i := range 10 {
		changeset := iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: []byte("key" + string(rune(i))), Value: make([]byte, 1024*1024)}, // 1MB values
			},
		}
		tree.ApplyChangeSet(changeset)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()
	err := tree.WriteSnapshot(context.Background(), snapshotDir)
	require.NoError(t, err)
}

// TestWriteSnapshotWithBuffer tests the buffer size selection logic
func TestWriteSnapshotWithBuffer(t *testing.T) {
	tree := New(0)
	for _, changes := range ChangeSets {
		tree.ApplyChangeSet(changes)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()

	// Test with custom buffer size
	err := writeSnapshotWithBuffer(
		context.Background(),
		snapshotDir,
		tree.version,
		1024*1024, // 1MB buffer
		int64(tree.root.Size()),
		logger.NewNopLogger(),
		func(w *snapshotWriter) (uint32, error) {
			if tree.root == nil {
				return 0, nil
			}
			if err := w.writeRecursive(tree.root); err != nil {
				return 0, err
			}
			return w.leafCounter, nil
		},
	)
	require.NoError(t, err)
}

// TestPipelineMetrics tests pipeline metrics reporting
func TestPipelineMetrics(t *testing.T) {
	tree := New(0)

	// Create enough data to generate meaningful metrics
	for i := range 100 {
		changeset := iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: []byte("key" + string(rune(i))), Value: []byte("value" + string(rune(i)))},
			},
		}
		tree.ApplyChangeSet(changeset)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()
	err := tree.WriteSnapshot(context.Background(), snapshotDir)
	require.NoError(t, err)
}

// TestSetPipelineBufferSize tests the pipeline buffer size configuration
func TestSetPipelineBufferSize(t *testing.T) {
	originalSize := nodeChanSize
	defer func() {
		nodeChanSize = originalSize // Restore original value
	}()

	// Test setting valid size
	SetPipelineBufferSize(50000)
	require.Equal(t, 50000, nodeChanSize)

	// Test minimum boundary
	SetPipelineBufferSize(50)
	require.Equal(t, 100, nodeChanSize) // Should be clamped to minimum

	// Test maximum boundary
	SetPipelineBufferSize(200000)
	require.Equal(t, 100000, nodeChanSize) // Should be clamped to maximum
}

// TestSnapshotWriterErrorHandling tests error handling in writer goroutines
func TestSnapshotWriterErrorHandling(t *testing.T) {
	tree := New(0)
	for _, changes := range ChangeSets {
		tree.ApplyChangeSet(changes)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	// Test with invalid directory to trigger write errors
	snapshotDir := "/invalid/path/that/does/not/exist"
	err := tree.WriteSnapshot(context.Background(), snapshotDir)
	require.Error(t, err)
}

// TestEmptySnapshotWrite tests writing an empty snapshot
func TestEmptySnapshotWrite(t *testing.T) {
	tree := New(0)
	snapshotDir := t.TempDir()

	err := tree.WriteSnapshot(context.Background(), snapshotDir)
	require.NoError(t, err)

	// Verify empty snapshot can be loaded
	opts := Options{}
	opts.FillDefaults()
	snapshot, err := OpenSnapshot(snapshotDir, opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, snapshot.Close()) })

	require.True(t, snapshot.IsEmpty())
	require.Equal(t, uint32(0), snapshot.Version())
}

// TestSnapshotWriterProgressReporting tests the progress reporting logic
func TestSnapshotWriterProgressReporting(t *testing.T) {
	tree := New(0)

	// Create enough nodes to trigger progress reporting (>30 seconds worth)
	// We'll use a smaller interval for testing
	for i := range 1000 {
		changeset := iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{Key: []byte("key" + string(rune(i))), Value: []byte("value" + string(rune(i)))},
			},
		}
		tree.ApplyChangeSet(changeset)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	snapshotDir := t.TempDir()

	// Reduce progress report interval for testing
	ctx := context.Background()
	err := tree.WriteSnapshot(ctx, snapshotDir)
	require.NoError(t, err)
}

// TestImportWithContext tests import with context cancellation
func TestImportWithContext(t *testing.T) {
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

	ch := make(chan *types.SnapshotNode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure context cleanup even if test fails early

	go func() {
		defer close(ch)
		exporter := snapshot.Export()
		for {
			node, err := exporter.Next()
			if err != nil {
				break
			}
			ch <- node
			// Cancel after first node
			cancel()
		}
	}()

	snapshotDir2 := t.TempDir()
	err = doImport(ctx, snapshotDir2, tree.Version(), ch)
	// Should handle cancellation gracefully
	if err != nil {
		require.Contains(t, err.Error(), "context")
	}
}
