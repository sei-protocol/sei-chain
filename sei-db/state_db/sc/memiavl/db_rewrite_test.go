package memiavl

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// TestMultiTreeWriteSnapshotPriorityEVM tests the priority EVM write strategy
func TestMultiTreeWriteSnapshotPriorityEVM(t *testing.T) {
	dir := t.TempDir()

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"evm", "bank", "acc"},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	// Apply changes to all stores
	for _, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{Name: "evm", Changeset: changes},
			{Name: "bank", Changeset: changes},
			{Name: "acc", Changeset: changes},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		_, err := db.Commit()
		require.NoError(t, err)
	}

	// Create snapshot - should use priority EVM strategy
	snapshotDir := filepath.Join(dir, "test-snapshot")
	err = db.MultiTree.WriteSnapshot(context.Background(), snapshotDir, db.snapshotWriterPool)
	require.NoError(t, err)

	// Verify all trees were written
	for _, store := range []string{"evm", "bank", "acc"} {
		storePath := filepath.Join(snapshotDir, store)
		_, err := os.Stat(storePath)
		require.NoError(t, err, "store %s should exist", store)
	}
}

// TestLoadMultiTreeWithPrefetchDisabled tests loading with prefetch disabled in background
func TestLoadMultiTreeWithPrefetchDisabled(t *testing.T) {
	dir := t.TempDir()

	// Create a DB with data
	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)

	for _, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{Name: "test", Changeset: changes},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		_, err := db.Commit()
		require.NoError(t, err)
	}

	// Create snapshot
	require.NoError(t, db.RewriteSnapshot(context.Background()))
	db.Close()

	// Reload with prefetch disabled (simulating background load)
	opts := Options{
		Config: Config{SnapshotPrefetchThreshold: 0}, // Disable prefetch
		Dir:    dir,
	}

	db2, err := OpenDB(logger.NewNopLogger(), 0, opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db2.Close()) })

	// Verify data is accessible
	tree := db2.TreeByName("test")
	require.NotNil(t, tree)
}
