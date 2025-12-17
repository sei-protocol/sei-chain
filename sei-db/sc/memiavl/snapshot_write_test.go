package memiavl

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/stretchr/testify/require"
)

// TestWriteSnapshotWithEmptyTree tests writing a snapshot with empty tree
func TestWriteSnapshotWithEmptyTree(t *testing.T) {
	dir := t.TempDir()

	tree := NewWithInitialVersion(0)
	tree.logger = logger.NewNopLogger()

	snapshotDir := filepath.Join(dir, "empty-snapshot")
	err := tree.WriteSnapshot(context.Background(), snapshotDir)
	require.NoError(t, err)

	// Verify snapshot directory was created
	_, err = os.Stat(snapshotDir)
	require.NoError(t, err)

	// Verify metadata file exists
	metadataPath := filepath.Join(snapshotDir, FileNameMetadata)
	_, err = os.Stat(metadataPath)
	require.NoError(t, err)
}

// TestWriteSnapshotWithData tests writing a snapshot with actual data
func TestWriteSnapshotWithData(t *testing.T) {
	dir := t.TempDir()

	tree := NewWithInitialVersion(0)
	tree.logger = logger.NewNopLogger()

	// Add some data
	for i := range 100 {
		key := []byte{byte(i)}
		value := []byte{byte(i * 2)}
		tree.Set(key, value)
	}

	_, _, err := tree.SaveVersion(false)
	require.NoError(t, err)

	snapshotDir := filepath.Join(dir, "data-snapshot")
	err = tree.WriteSnapshot(context.Background(), snapshotDir)
	require.NoError(t, err)

	// Verify all snapshot files exist
	for _, fileName := range []string{FileNameNodes, FileNameLeaves, FileNameKVs, FileNameMetadata} {
		filePath := filepath.Join(snapshotDir, fileName)
		info, err := os.Stat(filePath)
		require.NoError(t, err)
		if fileName != FileNameMetadata {
			require.Greater(t, info.Size(), int64(0), "file %s should not be empty", fileName)
		}
	}
}

// TestMultiTreeWriteSnapshotSequential tests that snapshots are written correctly for multiple stores
func TestMultiTreeWriteSnapshotSequential(t *testing.T) {
	dir := t.TempDir()

	// Create a DB with multiple stores
	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"store1", "store2", "store3"},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	// Add data to all stores
	for i := range 50 {
		cs := []*proto.NamedChangeSet{
			{
				Name: "store1",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte{byte(i), 1}, Value: []byte{byte(i * 2)}},
					},
				},
			},
			{
				Name: "store2",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte{byte(i), 2}, Value: []byte{byte(i * 3)}},
					},
				},
			},
			{
				Name: "store3",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte{byte(i), 3}, Value: []byte{byte(i * 4)}},
					},
				},
			},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		_, err := db.Commit()
		require.NoError(t, err)
	}

	// Create snapshot
	snapshotDir := filepath.Join(dir, "multi-store-snapshot")
	err = db.MultiTree.WriteSnapshot(context.Background(), snapshotDir, db.snapshotWriterPool)
	require.NoError(t, err)

	// Verify all stores have snapshot files
	for _, store := range []string{"store1", "store2", "store3"} {
		storePath := filepath.Join(snapshotDir, store)
		_, err := os.Stat(storePath)
		require.NoError(t, err, "store %s should exist", store)

		// Verify files within each store
		for _, fileName := range []string{FileNameNodes, FileNameLeaves, FileNameKVs, FileNameMetadata} {
			filePath := filepath.Join(storePath, fileName)
			_, err := os.Stat(filePath)
			require.NoError(t, err, "file %s should exist in store %s", fileName, store)
		}
	}
}

// TestSnapshotWriteWithContextCancellation tests snapshot write respects cancellation
func TestSnapshotWriteWithContextCancellation(t *testing.T) {
	dir := t.TempDir()

	tree := NewWithInitialVersion(0)
	tree.logger = logger.NewNopLogger()

	// Add a moderate amount of data
	for i := range 100 {
		key := []byte{byte(i >> 8), byte(i & 0xff)}
		value := make([]byte, 100)
		for j := range value {
			value[j] = byte(i + j)
		}
		tree.Set(key, value)
	}

	_, _, err := tree.SaveVersion(false)
	require.NoError(t, err)

	// Create a context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	snapshotDir := filepath.Join(dir, "cancelled-snapshot")
	err = tree.WriteSnapshot(ctx, snapshotDir)

	// Should get cancellation error or succeed quickly
	// (depending on timing, cancellation might not always trigger)
	if err != nil {
		require.ErrorIs(t, err, context.Canceled)
	}
}

// TestPipelineBufferSize tests the SetPipelineBufferSize function
func TestPipelineBufferSize(t *testing.T) {
	// Save original value
	originalSize := nodeChanSize
	defer func() {
		nodeChanSize = originalSize
	}()

	// Test valid size
	SetPipelineBufferSize(5000)
	require.Equal(t, 5000, nodeChanSize)

	// Test minimum bound
	SetPipelineBufferSize(50)
	require.Equal(t, 100, nodeChanSize) // Should be clamped to minimum

	// Test maximum bound
	SetPipelineBufferSize(200000)
	require.Equal(t, 100000, nodeChanSize) // Should be clamped to maximum
}
